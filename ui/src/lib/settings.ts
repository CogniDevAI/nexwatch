import pb from "@/lib/pocketbase";

interface SettingsRecord {
  id: string;
  key: string;
  value: string;
}

/**
 * Upsert one "settings" record, JSON-encoding value into its "value"
 * field — the hub's Go side (internal/hub/api/settings.go) always
 * json.Unmarshal's that field, so a string setting must be stored as a
 * quoted JSON string (`"my title"`), not the bare text. Used by the
 * Status page / Weekly report / Prometheus settings sections; the
 * pre-existing retention/collection-interval fields on this page keep
 * their own local helper (see below Settings component) since a number
 * happens to already read the same either way and changing it isn't
 * this feature's concern.
 */
export async function upsertSetting(key: string, value: unknown): Promise<void> {
  const encoded = JSON.stringify(value);
  try {
    const existing = await pb
      .collection("settings")
      .getFirstListItem<{ id: string }>(`key = '${key}'`);
    await pb.collection("settings").update(existing.id, { value: encoded });
  } catch {
    await pb.collection("settings").create({ key, value: encoded });
  }
}

/**
 * Fetch every "settings" record and decode each JSON-encoded value into a
 * plain key -> value map, skipping any record whose value fails to parse.
 * Lets a settings panel load its whole slice of keys in one request
 * instead of one getFirstListItem per key.
 */
export async function loadSettingsMap(): Promise<Record<string, unknown>> {
  const records = await pb.collection("settings").getFullList<SettingsRecord>({});
  const map: Record<string, unknown> = {};
  for (const record of records) {
    try {
      map[record.key] = JSON.parse(record.value);
    } catch {
      // Malformed value for this key — leave it absent from the map so
      // callers fall back to their own default rather than crashing.
    }
  }
  return map;
}

/** Type-narrowing helper: returns value if it's a string, else fallback. */
export function asString(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

/** Type-narrowing helper: returns value if it's a boolean, else fallback. */
export function asBool(value: unknown, fallback = false): boolean {
  return typeof value === "boolean" ? value : fallback;
}

/** Type-narrowing helper: returns value if it's a finite number, else fallback. */
export function asNumber(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}
