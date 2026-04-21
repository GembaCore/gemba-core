// Silences "not configured to support act(...)" warnings in React 18 when
// using act() outside of @testing-library/react.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
