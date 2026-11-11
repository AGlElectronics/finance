import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { Pagination } from './Pagination';

describe('Pagination', () => {
  it('hides itself when everything fits on one page', () => {
    const { container } = render(<Pagination total={10} offset={0} limit={50} onChange={() => {}} />);
    expect(container.innerHTML).toBe('');
  });

  it('shows the range and moves by one page', () => {
    const onChange = vi.fn();
    render(<Pagination total={120} offset={50} limit={50} onChange={onChange} />);
    expect(screen.getByText('51–100 of 120')).toBeTruthy();
    fireEvent.click(screen.getByText('Next'));
    fireEvent.click(screen.getByText('Previous'));
    expect(onChange.mock.calls).toEqual([[100], [0]]);
  });

  it('disables Next on the last page', () => {
    render(<Pagination total={120} offset={100} limit={50} onChange={() => {}} />);
    expect((screen.getByText('Next') as HTMLButtonElement).disabled).toBe(true);
  });
});
