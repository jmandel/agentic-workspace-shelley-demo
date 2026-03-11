export interface ClientIdentity {
  subject: string;
  displayName: string;
  token: string;
}

const SUBJECT_KEY = "workspace-auth-subject";
const DISPLAY_NAME_KEY = "workspace-display-name";

function randomId(prefix: string): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`;
}

function base64UrlEncode(value: unknown): string {
  const json = JSON.stringify(value);
  const bytes = new TextEncoder().encode(json);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function mintUnsignedJWT(subject: string, displayName: string): string {
  const now = Math.floor(Date.now() / 1000);
  const header = { alg: "none", typ: "JWT" };
  const claims = {
    iss: "workspace-demo",
    sub: subject,
    name: displayName,
    iat: now,
  };
  return `${base64UrlEncode(header)}.${base64UrlEncode(claims)}.`;
}

function normalizeDisplayName(value: string, fallback: string): string {
  return value.trim().replace(/\s+/g, " ").slice(0, 64) || fallback;
}

export function loadClientIdentity(): ClientIdentity {
  const storedSubject = (localStorage.getItem(SUBJECT_KEY) ?? "").trim();
  const subject = storedSubject || randomId("web");
  if (!storedSubject) {
    localStorage.setItem(SUBJECT_KEY, subject);
  }

  const storedDisplayName = normalizeDisplayName(localStorage.getItem(DISPLAY_NAME_KEY) ?? "", subject);
  if (!localStorage.getItem(DISPLAY_NAME_KEY)) {
    localStorage.setItem(DISPLAY_NAME_KEY, storedDisplayName);
  }

  return {
    subject,
    displayName: storedDisplayName,
    token: mintUnsignedJWT(subject, storedDisplayName),
  };
}

export function updateClientDisplayName(value: string): ClientIdentity {
  const current = loadClientIdentity();
  const displayName = normalizeDisplayName(value, current.subject);
  localStorage.setItem(DISPLAY_NAME_KEY, displayName);
  return {
    subject: current.subject,
    displayName,
    token: mintUnsignedJWT(current.subject, displayName),
  };
}

export function authorizationHeader(): Record<string, string> {
  return { Authorization: `Bearer ${loadClientIdentity().token}` };
}

export function socketAuthenticationMessage(): string {
  return JSON.stringify({
    type: "authenticate",
    token: loadClientIdentity().token,
  });
}
