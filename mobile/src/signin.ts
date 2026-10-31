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
  noAccount: "No account found.",
  create: "Create one",
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
 * The form of an email, the same loose check as the auth service on branch services (auth/auth.go, emailForm):
 * something@something.something, no spaces.
 */
export const EMAIL_FORM = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** The email as the services keep it: no spaces at the ends. */
export function cleanEmail(email: string): string {
  return email.trim();
}

/** The first problem of the form, or "" when it can go to the services. */
export function formProblem(email: string, password: string): string {
  const e = cleanEmail(email);
  if (e.length === 0 || password.length < MIN_PASSWORD) return SIGN_IN.incomplete;
  if (!EMAIL_FORM.test(e)) return SIGN_IN.badEmail;
  return "";
}

/** What a failed request shows: the offer to create an account (a 401 on sign-in) or a line of text. */
export function failure(mode: "login" | "signup", status: number | undefined, message: string): { offer: boolean; text: string } {
  if (mode === "login" && status === 401) return { offer: true, text: "" };
  return { offer: false, text: message };
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
