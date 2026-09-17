// UX-16: `navigator.clipboard.writeText` was awaited with no failure path
// anywhere it was used -- a denied clipboard permission, a non-secure
// context, or a browser that simply lacks the API left the copy button
// silently unchanged (button never said "Copied", but never said anything
// else either), with no way for the person to actually get the text short
// of manually selecting it themselves without being told to.
//
// copyToClipboard tries the modern API first, falls back to the legacy
// (still broadly supported, synchronous) execCommand("copy") technique via
// a throwaway textarea, and reports which happened so the caller can give
// real feedback either way.
export type CopyOutcome = "clipboard-api" | "legacy-fallback" | "failed";

export async function copyToClipboard(text: string): Promise<CopyOutcome> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return "clipboard-api";
    } catch {
      // Fall through to the legacy path below.
    }
  }
  try {
    const textarea = document.createElement("textarea");
    textarea.value = text;
    // Keep it present in the document (execCommand needs a real selection)
    // but visually and functionally out of the way.
    textarea.setAttribute("readonly", "");
    textarea.style.position = "fixed";
    textarea.style.top = "-1000px";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.select();
    textarea.setSelectionRange(0, text.length);
    const ok = document.execCommand("copy");
    textarea.remove();
    return ok ? "legacy-fallback" : "failed";
  } catch {
    return "failed";
  }
}

// selectText selects `element`'s text content in the page, so a failed
// copy still leaves the person able to copy manually (Ctrl/Cmd+C) without
// hunting for what to select themselves.
export function selectText(element: Element): void {
  const range = document.createRange();
  range.selectNodeContents(element);
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}
