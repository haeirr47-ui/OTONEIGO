const focusableSelector = 'button, a[href], input, select, textarea, [tabindex], [contenteditable="true"]';

export function isOverlayTabStop(element: HTMLElement): boolean {
  return (
    element.tabIndex >= 0 &&
    !element.matches(":disabled") &&
    !element.closest("[hidden], [inert]") &&
    element.getClientRects().length > 0 &&
    getComputedStyle(element).visibility === "visible"
  );
}

export function nextOverlayTabStop<T>(stops: T[], current: T | null, backwards: boolean): T | undefined {
  if (!stops.length) return undefined;
  const index = current === null ? -1 : stops.indexOf(current);
  if (index < 0) return backwards ? stops[stops.length - 1] : stops[0];
  return stops[(index + (backwards ? -1 : 1) + stops.length) % stops.length];
}

type OverlayOwner = object;
const owners = new WeakMap<Document, OverlayOwner[]>();
const inertOwners = new WeakMap<HTMLElement, { count: number; original: boolean }>();

// Lyrics leave the footer usable; the queue owns its whole keyboard scope.
// Restore pre-existing inert values rather than clearing another owner's state.
export function containOverlayFocus(
  roots: HTMLElement[],
  container: HTMLElement,
  initial: HTMLElement,
  close: () => void,
): () => void {
  const document = container.ownerDocument;
  const owner = {};
  const stack = owners.get(document) ?? [];
  owners.set(document, stack);
  stack.push(owner);
  const isTop = () => stack.at(-1) === owner;
  const background = Array.from(container.children)
    .filter((child): child is HTMLElement => child instanceof HTMLElement)
    .filter((child) => !roots.includes(child) && !child.hasAttribute("data-overlay-backdrop"));
  background.forEach((element) => {
    const entry = inertOwners.get(element) ?? { count: 0, original: element.inert };
    entry.count++;
    inertOwners.set(element, entry);
    element.inert = true;
  });
  const stops = () =>
    roots.flatMap((root) => Array.from(root.querySelectorAll<HTMLElement>(focusableSelector)).filter(isOverlayTabStop));
  const focusInside = () => (stops()[0] || initial).focus();
  const onKey = (event: KeyboardEvent) => {
    if (!isTop()) return;
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
    if (isTop() && !roots.some((root) => root.contains(event.target as Node))) focusInside();
  };
  document.addEventListener("keydown", onKey, true);
  document.addEventListener("focusin", onFocus);
  initial.focus();
  let disposed = false;
  return () => {
    if (disposed) return;
    disposed = true;
    const index = stack.indexOf(owner);
    if (index >= 0) stack.splice(index, 1);
    document.removeEventListener("keydown", onKey, true);
    document.removeEventListener("focusin", onFocus);
    background.forEach((element) => {
      const entry = inertOwners.get(element);
      if (!entry) return;
      if (--entry.count === 0) {
        element.inert = entry.original;
        inertOwners.delete(element);
      }
    });
  };
}
