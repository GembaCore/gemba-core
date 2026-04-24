// Silences "not configured to support act(...)" warnings in React 18 when
// using act() outside of @testing-library/react.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom returns zero layout dimensions, so @tanstack/react-virtual
// (used by WorkItemGrid) can't compute a visible window and skips
// rendering every row. Stub enough of the layout APIs that
// virtualization behaves as if the scroll container is 600px tall,
// which is enough for tests to assert on rendered-row counts.
const VIEWPORT_HEIGHT = 600;

const _origBoundingRect = Element.prototype.getBoundingClientRect;
Element.prototype.getBoundingClientRect = function () {
  return {
    height: VIEWPORT_HEIGHT,
    width: 1024,
    top: 0,
    left: 0,
    bottom: VIEWPORT_HEIGHT,
    right: 1024,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  } as DOMRect;
};
// Silence the lint about the unused original; callers that need the
// jsdom default can restore via Object.defineProperty.
void _origBoundingRect;

Object.defineProperty(Element.prototype, 'clientHeight', {
  configurable: true,
  get() {
    return VIEWPORT_HEIGHT;
  },
});
Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
  configurable: true,
  get() {
    return VIEWPORT_HEIGHT;
  },
});
