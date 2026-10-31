// The pickup address with Google Maps, written new for V3RDE (X49j, #80; T9 2026-10-31, the approved "V3RDE Give And
// Review Mockup" v2): the giver types the address, the app shows the one best suggestion (Places Autocomplete, New),
// the giver picks it, then the address locks and a small map shows the pin. Web only: the Maps JavaScript API is a
// browser script; the key comes from the listings service (GET /api/listings/config), never from the app bundle.
// The pure parts (the script URL, the one suggestion) have no browser and no network, for the tests.

/** The pickup address a giver chose: its text and its point (the pin, the distance, rescue). X49q: no ZIP. */
export type PickedAddress = { text: string; lat: number; lng: number };
/** The one suggestion on view: the two lines of the mockup ("12 Main St" / "Denver, CO 80219"). */
export type Suggestion = { main: string; secondary: string; pick: () => Promise<PickedAddress> };

/** The map zoom of the locked address (a few streets around the pin). */
export const MAP_ZOOM = 16;
/** The wait after the last key before a suggestion is asked for (fewer requests while the user types). */
export const SUGGEST_WAIT_MS = 300;
/** The fewest characters before a suggestion is asked for. */
export const SUGGEST_MIN = 4;
/** X49j review: the longest wait for the Maps script; then the review shows the place row of X49e. */
export const MAPS_LOAD_MS = 10_000;

/** The script URL of the Maps JavaScript API (https://developers.google.com/maps/documentation/javascript/load-maps-js-api). */
export function scriptUrl(key: string, callback: string): string {
  return "https://maps.googleapis.com/maps/api/js?key=" + encodeURIComponent(key) + "&v=weekly&loading=async&callback=" + callback;
}

type Prediction = { mainText?: { text: string } | null; secondaryText?: { text: string } | null; text?: { text: string } | null;
  toPlace: () => unknown };

/** T9: "there is only one suggestion given": the first place prediction, or null. */
export function firstPrediction(suggestions: Array<{ placePrediction?: Prediction | null }> | null | undefined): Prediction | null {
  for (const s of suggestions ?? []) if (s.placePrediction) return s.placePrediction;
  return null;
}

/** The two lines of a prediction (main, then the rest). */
export function linesOf(p: Prediction): { main: string; secondary: string } {
  return { main: p.mainText?.text ?? p.text?.text ?? "", secondary: p.secondaryText?.text ?? "" };
}

/** The two lines of a chosen address: the street, then the rest ("12 Main St" / "Denver, CO 80219, USA"). */
export function addressLines(text: string): { main: string; rest: string } {
  const k = text.indexOf(",");
  return k < 0 ? { main: text, rest: "" } : { main: text.slice(0, k).trim(), rest: text.slice(k + 1).trim() };
}

// ---- the browser part --------------------------------------------------------------------------------------------

type Maps = { importLibrary: (name: string) => Promise<any> };
let loading: Promise<Maps> | null = null;

/** Loads the Maps JavaScript API one time (web). A failure lets the next call try again. */
export function loadMaps(key: string): Promise<Maps> {
  const w = globalThis as unknown as { document?: Document; google?: { maps?: Maps }; __v3rdeMapsReady?: () => void };
  if (w.google?.maps?.importLibrary) return Promise.resolve(w.google.maps);
  if (loading) return loading;
  if (!w.document || !key) return Promise.reject(new Error("no map here"));
  loading = new Promise<Maps>((resolve, reject) => {
    const late = setTimeout(() => reject(new Error("the map did not load in time")), MAPS_LOAD_MS);
    w.__v3rdeMapsReady = () => { clearTimeout(late); return w.google?.maps ? resolve(w.google.maps) : reject(new Error("the map did not load")); };
    const s = w.document!.createElement("script");
    s.src = scriptUrl(key, "__v3rdeMapsReady");
    s.async = true;
    s.onerror = () => reject(new Error("the map did not load"));
    w.document!.head.appendChild(s);
  });
  loading.catch(() => { loading = null; });
  return loading;
}

/** One autocomplete session (a token for billing: https://developers.google.com/maps/documentation/javascript/place-autocomplete-data). */
export async function newSession(key: string): Promise<unknown> {
  const { AutocompleteSessionToken } = await (await loadMaps(key)).importLibrary("places");
  return new AutocompleteSessionToken();
}

/** X49q (T9 2026-10-31: "can the user set the location on a map and have it be another way to update the address?
 *  ... yes"): a map move changes the address text only when the pin moved more than this (fewer lookups). */
export const ADDRESS_FOLLOW_M = 20;

/** X49q review: the wait after a map stops before its address is asked (a second move in this time asks nothing). */
export const NAME_WAIT_MS = 500;
/** X49q review: the longest wait for that address; then the text is the coordinates (Post never waits longer). */
export const NAME_LIMIT_MS = 8000;

/** The distance between two points in metres (haversine, the mean Earth radius 6,371,008.8 m). */
export function metersBetween(a: { lat: number; lng: number }, b: { lat: number; lng: number }): number {
  const r = Math.PI / 180, R = 6_371_008.8;
  const dLat = (b.lat - a.lat) * r, dLng = (b.lng - a.lng) * r;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * r) * Math.cos(b.lat * r) * Math.sin(dLng / 2) ** 2;
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** The one suggestion for `input`, in any country (X49q: no region limit), or null. */
export async function suggest(key: string, input: string, session: unknown): Promise<Suggestion | null> {
  const { AutocompleteSuggestion } = await (await loadMaps(key)).importLibrary("places");
  const { suggestions } = await AutocompleteSuggestion.fetchAutocompleteSuggestions({ input, sessionToken: session });
  const p = firstPrediction(suggestions);
  if (!p) return null;
  return {
    ...linesOf(p),
    async pick() {
      const place = p.toPlace() as { fetchFields: (r: { fields: string[] }) => Promise<unknown>; formattedAddress?: string;
        location?: { lat: () => number; lng: () => number }; };
      await place.fetchFields({ fields: ["formattedAddress", "location"] });
      if (!place.location) throw new Error("the address has no point");
      return { text: place.formattedAddress ?? linesOf(p).main, lat: place.location.lat(), lng: place.location.lng() };
    },
  };
}

/** X49q (T9 2026-10-31, tester feedback: "Not able to drag the map ... i want move pin on map"): a map centred on
 *  `at` in `el` that the giver can move with one finger or the mouse (gestureHandling "greedy") and zoom (the zoom
 *  buttons); the pin of the app stays in its centre, so the point is the map centre. `onMove` gets the centre each
 *  time the map stops (the "idle" event). Answers `off` (removes the listener) and `hold` (no moves while posting). */
export type DrawnMap = { off: () => void; hold: (still: boolean) => void };
export async function drawMap(key: string, el: HTMLElement, at: { lat: number; lng: number },
  onMove: (p: { lat: number; lng: number }) => void): Promise<DrawnMap> {
  const { Map } = await (await loadMaps(key)).importLibrary("maps");
  const map = new Map(el, { center: { lat: at.lat, lng: at.lng }, zoom: MAP_ZOOM, disableDefaultUI: true, zoomControl: true,
    gestureHandling: "greedy", clickableIcons: false, keyboardShortcuts: false });
  const h = map.addListener("idle", () => {
    const c = map.getCenter();
    if (c) onMove({ lat: c.lat(), lng: c.lng() });
  });
  // X49q review: while the listing posts the map holds still (the pin shows the point that is posted)
  return { off: () => h.remove(), hold: (still) => map.setOptions({ gestureHandling: still ? "none" : "greedy", zoomControl: !still }) };
}

/** X49q: the address of a point (reverse geocoding, the Geocoding API of the same key), or "" when there is none.
 *  https://developers.google.com/maps/documentation/javascript/geocoding */
export async function addressAt(key: string, at: { lat: number; lng: number }): Promise<string> {
  const { Geocoder } = await (await loadMaps(key)).importLibrary("geocoding");
  const { results } = await new Geocoder().geocode({ location: { lat: at.lat, lng: at.lng } });
  return (results?.[0]?.formatted_address as string | undefined) ?? "";
}
