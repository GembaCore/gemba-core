import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  rendererFor,
  isKnownFormat,
  DESCRIPTION_FORMATS,
} from '../descriptionRenderers';
import { PlainDescription } from '../PlainDescription';

// The registry is the contract between the Go-declared DescriptionFormat
// constants (internal/core/workplane.go) and the SPA. These tests pin
// the fallback behaviour so an adaptor declaring a format the SPA
// doesn't yet know can't crash the drawer.

describe('rendererFor', () => {
  it('returns the markdown renderer for "markdown"', () => {
    const R = rendererFor('markdown');
    expect(R).not.toBe(PlainDescription);
  });

  it('returns the plain renderer for "plain"', () => {
    const R = rendererFor('plain');
    expect(R).toBe(PlainDescription);
  });

  it('falls back to plain for unknown values', () => {
    expect(rendererFor('asciidoc' as unknown as string)).toBe(PlainDescription);
    expect(rendererFor(undefined)).toBe(PlainDescription);
    expect(rendererFor(null)).toBe(PlainDescription);
    expect(rendererFor('')).toBe(PlainDescription);
  });
});

describe('PlainDescription', () => {
  it('preserves whitespace and newlines verbatim', () => {
    render(<PlainDescription source={'line one\nline two'} />);
    const pre = screen.getByTestId('description-plain');
    expect(pre.textContent).toBe('line one\nline two');
  });
});

describe('isKnownFormat / DESCRIPTION_FORMATS', () => {
  it('covers every value the registry lists', () => {
    expect(DESCRIPTION_FORMATS).toContain('plain');
    expect(DESCRIPTION_FORMATS).toContain('markdown');
    for (const f of DESCRIPTION_FORMATS) {
      expect(isKnownFormat(f)).toBe(true);
    }
    expect(isKnownFormat('unknown')).toBe(false);
  });
});
