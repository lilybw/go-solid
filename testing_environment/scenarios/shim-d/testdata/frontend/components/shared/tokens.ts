// A helper module in the components tree. A registry entry by extension, and
// no component at all.
export const SPACING = 8;

export function gap(n: number): string {
  return `${n * SPACING}px`;
}
