// V3RDE design tokens, written new for V3RDE (NEW_WORK_UI.md, UI 5; mobile/docs/style_guide.md).
// Each value is in mobile/docs/style_guide.md with its source: a measurement of the approved mockup shots
// (X32 and X25; mobile/tools/ui/measure_x32.py), a brand asset (Hungree red, Poppins), a platform rule (touch size,
// contrast), or a choice (marked so in the guide). Screens and parts take every colour, size and font from here;
// tests/style.test.mts checks that, and it checks that the guide lists every token with its value.
import * as RN from "react-native";

/** Colours for light and dark mode. tests/style.test.mts checks the text pairs (WCAG 2.1, 4.5:1 for text). */
export const palette = {
  light: {
    bg: "#F6F5F5",          // page behind the screens
    card: "#FFFFFF",        // cards, fields, the secondary pill
    fg: "#232323",          // text and icons
    muted: "#6D6D78",       // secondary text and labels
    placeholder: "#9F9DA4", // the example text in an empty field
    line: "#DDDDDD",        // borders of cards, fields and the secondary pill
    accent: "#BC202C",      // Hungree red: the primary pill, the progress bar
    onAccent: "#FFFFFF",    // text on the accent
    accentSoft: "#FFD4D7",  // an empty photo frame; the track of the progress bar
    danger: "#C43140",      // error text
    iconFill: "#F3F3F3",    // the disc of an icon button
    handle: "#DCDEDD",      // the grab bar of a sheet
    sheetTint: "rgba(255,255,255,0.55)", // the frosted layer of a sheet or dialog, over the blur
    blurTint: "light",                    // the expo-blur tint under it (a tint name, not a colour)
    shadeTop: "rgba(0,0,0,0)",            // the shade over the photo behind a sheet or dialog: top,
    shadeMid: "rgba(0,0,0,0.15)",         // at shade.mid of the height,
    shadeEnd: "rgba(0,0,0,0.7)",          // and at the bottom
    cameraBg: "#000000",                  // behind the live camera picture
    cameraBtn: "rgba(0,0,0,0.45)",       // buttons and the hint on the live camera
    cameraFg: "#FFFFFF",                  // their glyphs and text, and the shutter
    scrimDim: "rgba(0,0,0,0.43)",         // behind the camera dialog on a wide screen (#8C8C8C over bg, measured)
    link: "#BC202C",                      // text buttons and links (Show, Hide, Create one)
    doodle: "#BC202C",                    // the food doodles (at doodle.opacity)
    panel: "rgba(255,255,255,0.97)",      // the text panel on the photo of a listing card (almost solid: text stays clear)
  },
  dark: {
    bg: "#141414",
    card: "#1E1E20",
    fg: "#F3F3F3",
    muted: "#A1A1AA",
    placeholder: "#6D6D78",
    line: "#3A3A3F",
    accent: "#BC202C",
    onAccent: "#FFFFFF",
    accentSoft: "#4A1D22",
    danger: "#FF727D",
    iconFill: "#2A2A2E",
    handle: "#4A4A50",
    sheetTint: "rgba(20,20,22,0.62)",
    blurTint: "dark",
    shadeTop: "rgba(0,0,0,0)",
    shadeMid: "rgba(0,0,0,0.15)",
    shadeEnd: "rgba(0,0,0,0.7)",
    cameraBg: "#000000",
    cameraBtn: "rgba(0,0,0,0.45)",
    cameraFg: "#FFFFFF",
    scrimDim: "rgba(0,0,0,0.6)",
    link: "#FF727D",
    doodle: "#FF727D", // the earlier dark doodle colour
    panel: "rgba(30,30,32,0.97)",
  },
} as const;
export type Colors = { [K in keyof typeof palette.light]: string };

/** The colours of the device's mode (light when the mode is not known). */
export const useColors = (): Colors => (RN.useColorScheme() === "dark" ? palette.dark : palette.light);

/** Spacing in pt (measured: mobile/tools/ui/x32_measure.json). */
export const space = {
  hair: 4,     // a title to its muted line; a field label to its field
  inset: 6,    // card padding; the indent of a field label and of a note; a note under its field; above the photo row of a dialog;
               // the sign-in title to its line
  gap: 8,      // between the rows and controls inside a card; above an error line; the end of a sheet's scroll body
  item: 10,    // between cards; between two pills; a thumbnail to its fields; above a field label
  group: 12,   // above a stand-alone label (the "Servings" label of the one-item form)
  gutter: 16,  // screen edge to content (phone); a dialog to the window edge; the scroll body of a sheet to its pills;
               // the line of a small sheet to its pills; camera dialog padding
  foot: 20,    // the last pill to the bottom of a phone sheet; the photo to the list in a dialog; a small sheet title to
               // its pills (no line)
  pad: 24,     // padding of a dialog
  bar: 28,     // a sheet title to the progress bar under it
  grid: 14,    // between the listing cards of a screen (both ways)
} as const;

/** Corner radii in pt (measured). A pill is fully round. */
export const radius = {
  photo: 12,   // a photo inside a card (card 18 - inset 6: concentric)
  card: 18,    // cards, a field with several lines, the big photo of a dialog, the photo of the camera dialog
  sheet: 26,   // the top corners of a phone sheet; the corners of a dialog and of the camera dialog
  round: 999,  // pills, fields, icon buttons, the grab bar, the progress bar, the shutter, the camera hint
} as const;

/** Sizes in pt (measured; touch targets: Apple 44 pt, the small button is used only inside a card). */
export const size = {
  border: 1,
  control: 48,       // field and pill height
  controlTall: 72,   // a field with several lines
  fieldPadX: 18,     // field text to the field border (sides)
  fieldPadTop: 12,   // field text to the top border in a field with several lines
  pillPadX: 20,      // pill text to the pill border (sides), when the pill does not grow (a choice)
  iconSmall: 38,     // icon button inside a review card
  iconBig: 44,       // icon button of the one-item form
  thumb: 94,         // the item photo of a review card (square)
  handleW: 40,       // the grab bar of a sheet
  handleH: 4,
  handleTop: 10,     // sheet top to the grab bar
  sheetLarge: 0.85,  // a large phone sheet: at most this share of the screen height (else as tall as its content)
  sheetSmall: 210,   // a small phone sheet: at least this tall (its content starts at the top)
  sheetMedium: 0.44, // a medium phone sheet (the role sheet): this share of the screen height
  dialogWide: 860,   // a dialog with the photo on the left
  dialogExtraWide: 1140, // X49: the review dialog with the details column at the right (the photo, the list, the details)
  mapH: 160,         // X49j: the map of the pickup address on the review
  mapTall: 320,      // X49q: the same map while the giver changes the address (2 x mapH)
  chatColumn: 320,   // X49i: the chat column of the wide review (T9: wider than the X49 details column, 260)
  scanPhoto: 3 / 4,  // X49: the scan photo of the wide review (width to height); it is as high as the column
  dialogSmall: 440,  // a dialog with a title, a line and two pills
  dialogCamera: 640, // the camera dialog on a wide screen (a 4:3 photo, 608 pt wide)
  dialogMax: 0.869,  // a dialog is at most this share of the window height (782 of 900 pt measured)
  blur: 40,          // expo-blur intensity of the frosted layer
  progressH: 6,      // the progress bar
  focusBorder: 2,    // the border of a field while it has the focus (in accent)
  fieldSlot: 76,     // field text to the right border when a text button (Show) sits in the field
  loginTop: 0.14,    // the sign-in title from the top of a phone screen, as a share of the screen width
  header: 56,        // the ☰ header of a phone or tablet screen
  topBar: 64,        // the top bar of a wide screen (the brand, the role tabs, Log out)
  tabPadX: 16,       // a role tab and the Log out button of the top bar: text to the edge (sides)
  toastBottom: 88,   // a toast: its bottom to the screen bottom (over the bottom pill of a phone screen)
  toastBottomWide: 28, // the same on a wide screen (no bottom pill there)
  feedPhoto: 290,    // the photo of a listing card on Receive
  minePhoto: 220,    // the photo of a listing card on Give (Your listings)
  timeW: 88,         // the time pill of a listing card (one width for every time)
  timePadY: 2,       // its padding (top, bottom)
  loadAhead: 320,    // a list loads its next page when it is this near its end (pt)
  choiceThumb: 64,   // a photo to choose on the review (the original or the touched-up photo)
  proposalMax: 140,  // the lines of a voice proposal scroll above this height (the list stays in view on a phone)
} as const;

/**
 * The line glyphs, drawn as SVG paths with round ends (react-native-svg), in pt (measured from the X32 and X25
 * shots). Each path ends half a stroke inside its box, so the round ends touch the box edges.
 */
export const glyph = {
  box: 12,         // the square box of close, minus and plus
  uploadBox: 15,   // the square box of upload
  iconBox: 20,     // the square box of give (a camera), receive (a bowl) and menu
  stroke: 1.67,    // the line width
  close: "M0.835 0.835 L11.165 11.165 M11.165 0.835 L0.835 11.165",
  minus: "M0.835 6 H11.165",
  plus: "M0.835 6 H11.165 M6 0.835 V11.165",
  give: "M2.5 6.5 H6 L7.5 4 H12.5 L14 6.5 H17.5 A1.5 1.5 0 0 1 19 8 V15.5 A1.5 1.5 0 0 1 17.5 17 H2.5 A1.5 1.5 0 0 1 1 15.5 V8 A1.5 1.5 0 0 1 2.5 6.5 Z M10 9 A3 3 0 1 0 10 15 A3 3 0 1 0 10 9 Z",
  receive: "M1.5 9.5 H18.5 A8.5 6.5 0 0 1 1.5 9.5 Z M7 18.5 H13 M7.5 6.5 C7.5 5.2 8.5 5 8.5 3.5 M11.5 6.5 C11.5 5.2 12.5 5 12.5 3.5",
  menu: "M3 5.5 H17 M3 10 H17 M3 14.5 H17",
  mic: "M10 1.5 A3 3 0 0 1 13 4.5 V10 A3 3 0 0 1 7 10 V4.5 A3 3 0 0 1 10 1.5 Z M4 9.5 A6 6 0 0 0 16 9.5 M10 15.5 V18.5 M7 18.5 H13",
  sparkle: "M10 2 L11.6 8.4 L18 10 L11.6 11.6 L10 18 L8.4 11.6 L2 10 L8.4 8.4 Z", // X49: touch up
  info: "M10 1.5 A8.5 8.5 0 1 0 10 18.5 A8.5 8.5 0 1 0 10 1.5 Z M10 9 V14 M10 6 V6.2", // X49: details
  pencil: "M13.5 3 L17 6.5 L7 16.5 H3.5 V13 Z", // X49: change the details
  pin: "M10 18.5 C10 18.5 3.5 12 3.5 7.5 A6.5 6.5 0 0 1 16.5 7.5 C16.5 12 10 18.5 10 18.5 Z M10 5.5 A2 2 0 1 0 10 9.5 A2 2 0 1 0 10 5.5 Z", // X49j: the pickup pin
  stop: "M6.5 5 H13.5 A1.5 1.5 0 0 1 15 6.5 V13.5 A1.5 1.5 0 0 1 13.5 15 H6.5 A1.5 1.5 0 0 1 5 13.5 V6.5 A1.5 1.5 0 0 1 6.5 5 Z",
  upload: "M7.5 0.835 V10.8 M4.25 4 L7.5 0.835 L10.75 4 M0.835 11 V12.2 A2.2 2.2 0 0 0 3.035 14.165 H11.965 A2.2 2.2 0 0 0 14.165 12.2 V11",
} as const;

/** The shade over the photo behind a sheet or dialog: a vertical gradient (measured in all 23 X32/X25 shots). */
export const shade = {
  mid: 0.4, // the stop of shadeMid, as a share of the height
} as const;

/** The live camera screen (measured from the X25 scan mockup shots). */
export const camera = {
  edge: 14,          // the close button from the top-left corner
  side: 32,          // the upload button from the left edge
  uploadBottom: 48,  // the upload button bottom to the screen bottom (its centre 3 pt under the shutter centre)
  shutter: 76,       // shutter: outer diameter
  shutterRing: 5,    // its white ring
  shutterGap: 4,     // the gap between the ring and the white disc
  shutterBottom: 32, // shutter bottom to the screen bottom
  cardShutterBottom: 16, // shutter bottom to the photo bottom in the camera dialog
  hintBottom: 124,   // the hint pill bottom to the screen bottom
  hintPadX: 16,      // the hint pill padding (sides)
  hintPadY: 10,      // the hint pill padding (top, bottom)
  ratio: 4 / 3,      // the photo in the camera dialog (width to height)
} as const;

export const font = { // Poppins (SIL Open Font Licence, @expo-google-fonts/poppins): the Hungree brand font
  regular: "Poppins_400Regular",   // body text
  semibold: "Poppins_600SemiBold", // titles and pill text
} as const;

export const type = { // text styles (sizes measured from the glyph heights; line height measured for body text)
  title: { fontFamily: font.semibold, fontSize: 18, lineHeight: 24 },
  body: { fontFamily: font.regular, fontSize: 14, lineHeight: 20 },     // fields, labels, notes, errors
  button: { fontFamily: font.semibold, fontSize: 14, lineHeight: 20 },
  display: { fontFamily: font.semibold, fontSize: 22, lineHeight: 28 }, // the title of the About page (T9, Oct 30)
} as const;

/** Opacity of a control: pressed (a choice), busy (measured: the Post pill while posting, X32), hover (Ref-login). */
export const opacity = {
  pressed: 0.85, // while a finger is on the control
  busy: 0.6,     // while the control waits (the spinner shows)
  hover: 0.92,   // while the mouse is on a pill (web)
  gone: 0.6,     // a listing card that is collected or out of time
} as const;

/** Motion (a choice): a sheet rises with a spring; each row of it rises after the one before. */
export const motion = {
  rowStepMs: 40,    // the delay from one row to the next
  rowRise: 8,       // how far a row rises (pt)
  rowMs: 220,       // how long a row takes
  sheetSpeed: 14,   // the spring of a sheet (React Native Animated.spring speed)
  sheetBounce: 0,   // and its bounciness (no overshoot)
  sheetShift: 0.2,  // a sheet starts this share of the window height below its place
  handoffMs: 300,   // wide screens: the sign-in card fades out in this time after sign-in
  handoffLift: 40,  // and rises this far (pt)
  dimMs: 250,       // the dim layer behind a sheet over a screen fades in in this time
  growMs: 220,      // X49: the details box opens in this time; then its text types in
  growFrom: 0.96,   // X49: the size the details box opens from (a share of its size)
  letterMs: 14,     // X49: the time of one letter of the text that types in
  lineMs: 220,      // X49: the pause at the end of a line of the text that types in
} as const;

/**
 * The food doodles (a wallpaper): a line drawing in every cell of a grid; the cellular automaton rule 30 makes
 * each cell live or dead, and a drawing in a dead cell is fainter. Each step a new generation comes in at the
 * bottom and the pattern moves up one row (mobile/src/automata.ts, mobile/src/doodles.tsx).
 */
export const doodle = {
  opacity: 0.09,     // light mode
  opacityDark: 0.1,  // dark mode
  units: 24,         // the box of the drawing paths (path units)
  glyph: 32,         // the box of one drawing (pt)
  stroke: 2.2,       // the line width of a drawing (pt)
  cell: 38,          // the grid cell (pt): one drawing in each
  jitter: 3,         // the largest shift of a drawing from the centre of its cell (pt)
  turn: 20,          // the largest turn of a drawing (degrees)
  turnSteps: 2,      // turn steps on each side of 0 (so -20, -10, 0, 10, 20)
  seed: 1026,        // the start of the automaton (the same picture on every start)
  rowDensity: 0.5,   // the share of live cells in the random first row of rule 30
  faint: 0.55,       // the strength of a drawing in a dead cell (a drawing in a live cell has 1)
  stepMs: 1000,      // the time of one generation: the pattern moves up one row
} as const;

/**
 * The layout of a window width, after the Material 3 window size classes: compact (< 600 dp) shows bottom
 * sheets, one column of cards and the ☰ header; medium (600-839) shows centred dialogs, two columns and the ☰
 * header; expanded (>= 840) shows centred dialogs, three columns and the top bar with the role tabs. `maxWidth`
 * is the widest the content of a screen gets (it is centred in a wider window).
 */
export function layoutFor(width: number): {
  kind: "compact" | "medium" | "expanded"; dialogs: boolean; columns: number; topBar: boolean; maxWidth: number;
} {
  if (width >= 840) return { kind: "expanded", dialogs: true, columns: 3, topBar: true, maxWidth: 1120 };
  if (width >= 600) return { kind: "medium", dialogs: true, columns: 2, topBar: false, maxWidth: 900 };
  return { kind: "compact", dialogs: false, columns: 1, topBar: false, maxWidth: 560 };
}
