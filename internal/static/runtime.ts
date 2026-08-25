// The stable half of the generated module: the types every asset is described
// with, and the one loader they all share.
//
// Nothing here depends on what is in the asset directory, so editing an asset
// rewrites assets.ts and leaves this file untouched.

declare const MIME: unique symbol;

/**
 * The content-hashed URL of one static asset, carrying its media type.
 *
 * It is a string, so it drops into anything expecting one:
 *
 *     <img src={S.images.logo} />
 *
 * The brand is phantom — it exists only in the type system, never at runtime —
 * and is what lets load() know what it will hand back.
 */
export type AssetURL<M extends string = string> = string & { readonly [MIME]: M };

/** What load() resolves to, per media type. Anything unlisted gives a Blob. */
export interface AssetData {
  "application/json": unknown;
  "application/manifest+json": unknown;
  "image/svg+xml": string;
  "text/css": string;
  "text/csv": string;
  "text/html": string;
  "text/plain": string;
}

export type DataFor<M extends string> = M extends keyof AssetData ? AssetData[M] : Blob;

/**
 * A feature that was never switched on. Reaching into one is a type error
 * carrying the reason it is off and the setting that turns it on.
 */
export type FeatureDisabled<Reason extends string> = {
  readonly __GO_SOLID_DISABLED__: Reason;
};

async function fetchAsset(url: string, init?: RequestInit): Promise<Response> {
  const response = await fetch(url, init);
  if (!response.ok) {
    throw new Error("@go_solid/static: " + response.status + " for " + url);
  }
  return response;
}

/**
 * Fetch an asset, decoded by the media type the server reports.
 *
 * The decision is made from the response header rather than from a table baked
 * into this file: the server has to send it anyway, so there is no second
 * source to keep in step, and an asset whose type nothing anticipated still
 * decodes to something usable.
 */
export function load<M extends string>(url: AssetURL<M>, init?: RequestInit): Promise<DataFor<M>>;
export function load(url: string, init?: RequestInit): Promise<Blob>;
export async function load(url: string, init?: RequestInit): Promise<unknown> {
  const response = await fetchAsset(url, init);
  const type = (response.headers.get("content-type") || "").split(";")[0].trim();
  if (type === "application/json") return response.json();
  if (type.startsWith("text/") || type === "image/svg+xml") return response.text();
  return response.blob();
}

/** Fetch an asset as text, whatever its media type. */
export async function loadText(url: string, init?: RequestInit): Promise<string> {
  return (await fetchAsset(url, init)).text();
}

/** Fetch an asset as parsed JSON, whatever its media type. */
export async function loadJSON(url: string, init?: RequestInit): Promise<unknown> {
  return (await fetchAsset(url, init)).json();
}
