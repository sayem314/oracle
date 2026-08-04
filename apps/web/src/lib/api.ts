import { parseSSE } from "./sse";

export interface User {
  email: string;
  role?: string;
  [key: string]: unknown;
}

interface SessionResponse {
  user: User;
}

async function parseError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string; message?: string };
    if (body?.error) return body.error;
    if (body?.message) return body.message;
  } catch {
    // fall through to a generic message
  }
  return `request failed (${res.status})`;
}

async function postJSON(path: string, payload: unknown): Promise<Response> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res;
}

// Returns the signed-in user, or null when there is no valid session.
export async function getSession(): Promise<User | null> {
  const res = await fetch("/auth/me", { credentials: "include" });
  if (!res.ok) return null;
  const body = (await res.json()) as SessionResponse;
  return body?.user ?? null;
}

export async function signIn(credential: string, password: string): Promise<User> {
  const res = await postJSON("/auth/signin/credential", { credential, password });
  const body = (await res.json()) as SessionResponse;
  return body.user;
}

export async function signUp(email: string, password: string): Promise<User> {
  const res = await postJSON("/auth/signup/credential", { email, password });
  const body = (await res.json()) as SessionResponse;
  return body.user;
}

export async function signOut(): Promise<void> {
  await postJSON("/auth/signout", {});
}

export interface ChatStreamCallbacks {
  onStart?: (sessionId: number, userMessageId: number) => void;
  onDelta?: (content: string) => void;
  onDone?: (messageId: number, finishReason: string) => void;
  onError?: (message: string) => void;
}

export interface ChatOptions {
  sessionId: number | null;
  message: string;
  model?: string;
  signal?: AbortSignal;
}

// Streams a chat completion. Pre-stream failures (validation, auth) reject with
// an Error; mid-stream failures surface through cb.onError.
export async function streamChat(opts: ChatOptions, cb: ChatStreamCallbacks): Promise<void> {
  const res = await fetch("/api/v1/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
    body: JSON.stringify({ session_id: opts.sessionId, message: opts.message, model: opts.model }),
    credentials: "include",
    signal: opts.signal,
  });

  if (!res.ok || !res.body) {
    throw new Error(await parseError(res));
  }

  for await (const { event, data } of parseSSE(res.body)) {
    let payload: Record<string, unknown> = {};
    try {
      payload = JSON.parse(data) as Record<string, unknown>;
    } catch {
      // Ignore malformed payloads rather than killing the stream.
    }

    switch (event) {
      case "start":
        cb.onStart?.(Number(payload.session_id), Number(payload.user_message_id));
        break;
      case "delta":
        cb.onDelta?.(typeof payload.content === "string" ? payload.content : "");
        break;
      case "done":
        cb.onDone?.(Number(payload.message_id), typeof payload.finish_reason === "string" ? payload.finish_reason : "");
        break;
      case "error":
        cb.onError?.(typeof payload.message === "string" ? payload.message : "stream error");
        break;
    }
  }
}
