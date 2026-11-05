import { describe, expect, it } from 'vitest';

import { centsToInput, parseEuros } from './money';

describe('parseEuros', () => {
  it('accepts comma and dot decimals', () => {
    expect(parseEuros('12,50')).toBe(1250);
    expect(parseEuros('12.50')).toBe(1250);
    expect(parseEuros('0,05')).toBe(5);
    expect(parseEuros('100')).toBe(10000);
  });

  it('accepts negatives (discount lines)', () => {
    expect(parseEuros('-5')).toBe(-500);
  });

  it('rejects garbage', () => {
    expect(parseEuros('')).toBeNull();
    expect(parseEuros('abc')).toBeNull();
    expect(parseEuros('1,234')).toBeNull(); // more than 2 decimals
    expect(parseEuros('1.2.3')).toBeNull();
  });

  it('round-trips with centsToInput', () => {
    expect(parseEuros(centsToInput(1999))).toBe(1999);
  });
});
