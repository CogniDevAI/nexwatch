import PocketBase from "pocketbase";

// In production the UI is served by the hub itself, so use relative URL.
// In development use the env var or fallback to localhost.
const pbUrl =
  import.meta.env.VITE_PB_URL ||
  (import.meta.env.DEV ? "http://localhost:8090" : window.location.origin);

const pb = new PocketBase(pbUrl);

// Disable auto-cancellation so multiple concurrent requests don't cancel each other
pb.autoCancellation(false);

export default pb;
