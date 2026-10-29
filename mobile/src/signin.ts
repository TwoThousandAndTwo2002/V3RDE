// The words and the rules of the sign-in screen, written new for V3RDE (NEW_WORK_UI.md, UI 8).
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
} as const;

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
