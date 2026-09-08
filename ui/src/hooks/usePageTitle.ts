import { useEffect } from "react";

/** Sets `document.title` to "<title> — NexWatch" for the lifetime of the calling page. */
export function usePageTitle(title: string): void {
  useEffect(() => {
    const previous = document.title;
    document.title = `${title} — NexWatch`;
    return () => {
      document.title = previous;
    };
  }, [title]);
}
