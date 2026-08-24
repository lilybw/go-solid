async function fetchAsset(url, init) {
  const response = await fetch(url, init);
  if (!response.ok) {
    throw new Error("go-solid/static: " + response.status + " for " + url);
  }
  return response;
}

/** Fetch an asset, decoded by the media type the server reports. */
export async function load(url, init) {
  const response = await fetchAsset(url, init);
  const type = (response.headers.get("content-type") || "").split(";")[0].trim();
  if (type === "application/json") return response.json();
  if (type.startsWith("text/") || type === "image/svg+xml") return response.text();
  return response.blob();
}

/** Fetch an asset as text, whatever its media type. */
export async function loadText(url, init) {
  return (await fetchAsset(url, init)).text();
}

/** Fetch an asset as parsed JSON, whatever its media type. */
export async function loadJSON(url, init) {
  return (await fetchAsset(url, init)).json();
}