// A helper module that happens to live in the components tree. It is a registry
// entry by extension, but it backs no component at all.
export const SPACING = 8;

export type Tone = "muted" | "loud";

// technically a valid solidjs component cause JSX.Element includes string
export function classFor(tone: Tone): string {
  return tone === "loud" ? "t-loud" : "t-muted";
}
