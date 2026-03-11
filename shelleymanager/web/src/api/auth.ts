export interface ClientIdentity {
  subject: string;
  displayName: string;
  token: string;
}

const PARTICIPANT_ADJECTIVES = [
  "amber",
  "arcane",
  "brisk",
  "calm",
  "cedar",
  "cinder",
  "clear",
  "cloudy",
  "cobalt",
  "comet",
  "crisp",
  "daring",
  "dawn",
  "ember",
  "fern",
  "frost",
  "gentle",
  "glossy",
  "golden",
  "granite",
  "harbor",
  "hazy",
  "hidden",
  "hollow",
  "ivory",
  "juniper",
  "keen",
  "kind",
  "lively",
  "lunar",
  "maple",
  "meadow",
  "mellow",
  "misty",
  "noble",
  "opal",
  "orchid",
  "pebble",
  "pine",
  "quiet",
  "rapid",
  "river",
  "sage",
  "silver",
  "smoky",
  "solar",
  "spruce",
  "steady",
  "stellar",
  "summit",
  "swift",
  "tidy",
  "timber",
  "velvet",
  "verdant",
  "violet",
  "warm",
  "whisper",
  "winter",
];

const PARTICIPANT_NOUNS = [
  "badger",
  "beacon",
  "brook",
  "canopy",
  "canyon",
  "circuit",
  "cloud",
  "comet",
  "creek",
  "falcon",
  "field",
  "firefly",
  "fjord",
  "forest",
  "fox",
  "garden",
  "glacier",
  "grove",
  "harbor",
  "hawk",
  "heron",
  "hill",
  "iris",
  "island",
  "lagoon",
  "lantern",
  "leaf",
  "lily",
  "meadow",
  "mesa",
  "minnow",
  "moon",
  "otter",
  "owl",
  "path",
  "peak",
  "pine",
  "planet",
  "pond",
  "quartz",
  "raven",
  "reef",
  "ridge",
  "river",
  "rook",
  "shore",
  "sparrow",
  "spring",
  "star",
  "stone",
  "stream",
  "sun",
  "thicket",
  "trail",
  "vale",
  "wave",
  "willow",
  "wind",
  "wren",
];

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

function capitalize(value: string): string {
  if (!value) return value;
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export function generateParticipantDisplayName(
  random: () => number = Math.random,
): string {
  const adjective =
    PARTICIPANT_ADJECTIVES[
      Math.floor(random() * PARTICIPANT_ADJECTIVES.length)
    ] ?? "quiet";
  const noun =
    PARTICIPANT_NOUNS[Math.floor(random() * PARTICIPANT_NOUNS.length)] ??
    "otter";
  const number = 10000 + Math.floor(random() * 90000);
  return `${capitalize(adjective)} ${capitalize(noun)} ${number}`;
}

export function loadClientIdentity(): ClientIdentity {
  const storedSubject = (localStorage.getItem(SUBJECT_KEY) ?? "").trim();
  const subject = storedSubject || randomId("web");
  if (!storedSubject) {
    localStorage.setItem(SUBJECT_KEY, subject);
  }

  const storedDisplayName = normalizeDisplayName(
    localStorage.getItem(DISPLAY_NAME_KEY) ?? "",
    generateParticipantDisplayName(),
  );
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
  const displayName = normalizeDisplayName(value, current.displayName);
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
