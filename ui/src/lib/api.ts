import pb from "@/lib/pocketbase";

/**
 * apiFetch wraps the browser fetch API for calls to the hub's custom REST
 * endpoints under /api/custom/*. It resolves relative paths against the
 * PocketBase base URL, always attaches the current auth token, and — on a
 * 401 response — clears the session and redirects to /login so an expired
 * or revoked session doesn't strand the user on a broken page.
 */
export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const url = new URL(path, pb.baseUrl).toString();

  const headers = new Headers(init.headers);
  headers.set("Authorization", pb.authStore.token ?? "");

  const response = await fetch(url, { ...init, headers });

  if (response.status === 401) {
    pb.authStore.clear();
    window.location.href = "/login";
  }

  return response;
}
