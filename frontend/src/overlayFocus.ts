const focusableSelector =
  'button, a[href], input, select, textarea, [tabindex], [contenteditable="true"]';

export function isOverlayTabStop(element: HTMLElement): boolean {
  return element.tabIndex >= 0 &&
    !element.matches(":disabled") &&
    !element.closest("[hidden], [inert]") &&
    element.getClientRects().length > 0 &&
    getComputedStyle(element).visibility === "visible";
}

export function nextOverlayTabStop<T>(stops: T[], current: T | null, backwards: boolean): T | undefined {
  if (!stops.length) return undefined;
  const index = current === null ? -1 : stops.indexOf(current);
  if (index < 0) return backwards ? stops[stops.length - 1] : stops[0];
  return stops[(index + (backwards ? -1 : 1) + stops.length) % stops.length];
}

// Lyrics leave the footer usable; the queue owns its whole keyboard scope.
// Restore pre-existing inert values rather than clearing another owner's state.
export function containOverlayFocus(
  roots: HTMLElement[],
  container: HTMLElement,
  initial: HTMLElement,
  close: () => void,
): () => void {
  const document = container.ownerDocument;
  const background = Array.from(container.children)
    .filter((child): child is HTMLElement => child instanceof HTMLElement)
    .filter((child) => !roots.includes(child) && !child.hasAttribute("data-overlay-backdrop"));
  const previousInert = background.map((element) => element.inert);
  background.forEach((element) => { element.inert = true; });
  const stops = () => roots.flatMap((root) =>
    Array.from(root.querySelectorAll<HTMLElement>(focusableSelector)).filter(isOverlayTabStop),
  );
  const focusInside = () => (stops()[0] || initial).focus();
  const onKey = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      close();
    } else if (event.key === "Tab") {
      event.preventDefault();
      const next = nextOverlayTabStop(stops(), document.activeElement as HTMLElement, event.shiftKey);
      (next || initial).focus();
    }
  };
  const onFocus = (event: FocusEvent) => {
    if (!roots.some((root) => root.contains(event.target as Node))) focusInside();
  };
  document.addEventListener("keydown", onKey, true);
  document.addEventListener("focusin", onFocus);
  initial.focus();
  return () => {
    document.removeEventListener("keydown", onKey, true);
    document.removeEventListener("focusin", onFocus);
    background.forEach((element, index) => { element.inert = previousInert[index]; });
  };
}
