import PocketBase from "pocketbase";

const pb = new PocketBase(
  import.meta.env.VITE_PB_URL || "http://localhost:8090",
);

// Disable auto-cancellation so multiple concurrent requests don't cancel each other
pb.autoCancellation(false);

export default pb;
