// vitest global setup. Flags the environment as a React act() host so
// createRoot renders inside act(...) do not emit a warning. This MUST
// be set before any component under test imports React.
(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
