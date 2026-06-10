// SourceTiles tests (gm-uipx.7).

import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { SourceTiles } from '../SourceTiles';

describe('SourceTiles', () => {
  it('renders all four source tiles with stable testids', () => {
    render(<SourceTiles onSelect={() => {}} />);
    expect(screen.getByTestId('bootstrap-source-tiles')).toBeTruthy();
    for (const id of ['jira', 'beads', 'source-code', 'fresh']) {
      expect(screen.getByTestId(`bootstrap-source-tile-${id}`)).toBeTruthy();
    }
  });

  it('clicking a tile calls onSelect with the tile id', () => {
    const onSelect = vi.fn();
    render(<SourceTiles onSelect={onSelect} />);
    fireEvent.click(screen.getByTestId('bootstrap-source-tile-fresh'));
    expect(onSelect).toHaveBeenCalledWith('fresh');
  });

  it('each tile has a one-line description', () => {
    render(<SourceTiles onSelect={() => {}} />);
    expect(screen.getByText(/Import epics/)).toBeTruthy();
    expect(screen.getByText(/Adopt an existing beads database/)).toBeTruthy();
    expect(screen.getByText(/Scan a repository tree/)).toBeTruthy();
    expect(screen.getByText(/Start from scratch/)).toBeTruthy();
  });
});
