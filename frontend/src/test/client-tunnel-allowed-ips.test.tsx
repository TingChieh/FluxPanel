import { describe, it, expect } from 'vitest';

import { parseAllowedIPsList } from '@/pages/clients/ClientFormModal';

describe('parseAllowedIPsList', () => {
  it('splits, trims, and drops empty entries', () => {
    expect(parseAllowedIPsList(' 10.0.0.2/32 , 10.0.0.3/32,')).toEqual([
      '10.0.0.2/32',
      '10.0.0.3/32',
    ]);
  });

  it('returns an empty array for a blank string', () => {
    expect(parseAllowedIPsList('')).toEqual([]);
  });
});
