// The words and the rules of the sign-in and reset screens, written new for V3RDE (NEW_WORK_UI.md, UI 8 and 9).
// Kept out of the screen so the tests can check them.

/** The shortest password the services accept. */
export const MIN_PASSWORD = 8;

/** The words of the sign-in screen (T9, Oct 29: the earlier words, made simpler). */
export const SIGN_IN = {
  title: "Share or find a meal",
  line: "Sign in or sign up",
  email: "Email",
  emailHint: "you@email.com",
  password: "Password",
  rule: MIN_PASSWORD + " characters or more",
  incomplete: "Enter your email and a password of " + MIN_PASSWORD + " characters or more",
  badEmail: "Enter an email like you@email.com",
  waiting: "Signing in…",
  // X49w (tester: a wrong password showed "No account found. Create one"; T9 2026-11-01: "when an email is not
  // recognized ... render create an account ... if a password is wrong only then do you render forgot password"):
  // the auth service answers 401 with a code for each case (auth/handler.go CodeNoAccount, CodeWrongPassword)
  noAccount: "No account uses this email.",
  wrongPassword: "Wrong password.",
  badLogin: "Wrong email or password.", // a 401 with no code (an older service)
  create: "Create an account",
  go: "Continue",
  show: "Show",
  hide: "Hide",
  showName: "Show password",
  hideName: "Hide password",
  about: "About",
} as const;

/** The About page (NEW_WORK.md #78): a static page of the web build (mobile/public/about/), on the same site. */
export const ABOUT_PATH = "/about/";
/** The same page from a phone build, which has no site of its own (T9, Oct 30: the app is a web app for now). */
export const ABOUT_URL = "https://icarus.thehungreeapp.com/about/";

/**
 * The form of an email for a new account, the same check as the auth service (auth/auth.go, emailForm). X49w (T9:
 * "emails should be validated more strictly"; any country): the "valid e-mail address" of the HTML standard (the
 * browser's own check of an email field) plus a domain with a dot and an ending of 2 letters or more:
 * name@domain.com, name@company.ng, name@yahoo.co.uk; not name@domain, name@domain.c, spaces or two @.
 */
export const EMAIL_FORM = /^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*\.[A-Za-z]{2,63}$/;

/** X49w review: sign-in and the reset request keep the earlier loose check (auth emailLoose), so an account made
 *  before the strict form still signs in and gets a reset link: something@something.something, no spaces. */
export const EMAIL_LOOSE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** The email as the services keep it: no spaces at the ends. */
export function cleanEmail(email: string): string {
  return email.trim();
}

/** The first problem of the form, or "" when it can go to the services. X49w review: the strict form for sign-up only. */
export function formProblem(email: string, password: string, mode: "login" | "signup" = "login"): string {
  const e = cleanEmail(email);
  if (e.length === 0 || password.length < MIN_PASSWORD) return SIGN_IN.incomplete;
  if (!(mode === "signup" ? EMAIL_FORM : EMAIL_LOOSE).test(e)) return SIGN_IN.badEmail;
  return "";
}

/** X49w: the link under the fields after a failed sign-in: "Create an account", "Forgot password?" or none. */
export type Offer = "create" | "forgot" | "";

/** What a failed request shows: a line of text and the one link that helps (X49w: from the code of a 401 on sign-in). */
export function failure(mode: "login" | "signup", status: number | undefined, message: string, code?: unknown): { offer: Offer; text: string } {
  if (mode === "login" && status === 401) {
    if (code === "no_account") return { offer: "create", text: SIGN_IN.noAccount };
    if (code === "wrong_password") return { offer: "forgot", text: SIGN_IN.wrongPassword };
    return { offer: "forgot", text: SIGN_IN.badLogin };
  }
  return { offer: "", text: message };
}

// Reset password by email link (UI 9). The services part is on branch services (docs/sources/U9.md: the routes).

/** The link works this long, one time (the auth service sets it). */
export const RESET_MINUTES = 30;

/** The words of the reset screens (ASD-STE100). */
export const RESET = {
  forgot: "Forgot password?",
  askTitle: "Reset your password",
  askLine: "We send a link to your email",
  send: "Send link",
  sending: "Sending…",
  back: "Back to sign in",
  sentTitle: "Check your email",
  sent: (email: string) => "If an account uses " + email + ", a link to set a new password is on its way. It works for " + RESET_MINUTES + " minutes.",
  newTitle: "Set a new password",
  newLine: "Choose a password of " + MIN_PASSWORD + " characters or more",
  newPassword: "New password",
  save: "Save password",
  saving: "Saving…",
  short: "Use " + MIN_PASSWORD + " characters or more",
  expired: "This link has expired or was used.",
  again: "Send a new link",
} as const;

/** The answer of the auth service for a token that is unknown, used or too old (one message for all three). */
export const EXPIRED_ERROR = "reset link expired";

/**
 * The token of a reset link (web /reset#token=…, app v3rde://reset#token=…), or "" when the link is not one. The
 * token is after "#", so the browser does not send it to the web server or in a Referer header.
 */
export function resetToken(url: string | null | undefined): string {
  if (!url) return "";
  const path = url.startsWith("v3rde://") ? "/" + url.slice("v3rde://".length) : url.replace(/^https?:\/\/[^/?#]+/, "");
  const m = /^\/reset\/?(?:\?[^#]*)?#(?:[^#]*&)?token=([A-Za-z0-9_-]+)(?:&|$)/.exec(path);
  return m ? m[1] : "";
}

/**
 * N4 (T9 2026-10-31): the listing of a claim link (web /l/<listing_id>, app v3rde://l/<listing_id>), or "" when the
 * link is not one. The Facebook post of a listing (fan-out) ends with this link; after sign-in or sign-up the app
 * opens Receive and skips the role sheet. The id has the listings service's form (l_ and 16 hex characters).
 */
export function claimListing(url: string | null | undefined): string {
  if (!url) return "";
  const path = url.startsWith("v3rde://") ? "/" + url.slice("v3rde://".length) : url.replace(/^https?:\/\/[^/?#]+/, "");
  const m = /^\/l\/(l_[0-9a-f]{16})\/?(?:[?#].*)?$/.exec(path);
  return m ? m[1] : "";
}
