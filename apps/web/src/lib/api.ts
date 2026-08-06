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

async function patchJSON(path: string, payload: unknown): Promise<Response> {
  const res = await fetch(path, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return res;
}

async function deleteRequest(path: string): Promise<void> {
  const res = await fetch(path, { method: "DELETE", credentials: "include" });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
}

async function getRequest(path: string): Promise<Response> {
  const res = await fetch(path, { credentials: "include" });
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

export interface ToolCallInfo {
  id: string;
  name: string;
  arguments: string;
}

export interface ApprovalRequiredInfo {
  id: number;
  toolCallId: string;
  messageId: number;
  name: string;
  arguments: string;
}

export interface ChatStreamCallbacks {
  onStart?: (sessionId: number, userMessageId: number) => void;
  onDelta?: (content: string) => void;
  onDone?: (messageId: number, finishReason: string) => void;
  onError?: (message: string) => void;
  onToolCalls?: (calls: ToolCallInfo[]) => void;
  onToolResult?: (toolCallId: string, name: string, result: string) => void;
  onApprovalRequired?: (approval: ApprovalRequiredInfo) => void;
  onDecision?: (id: number, result: string, status: string) => void;
}

export interface ChatOptions {
  sessionId: number | null;
  message: string;
  model?: string;
  providerId?: number;
  signal?: AbortSignal;
}

// Streams a chat completion. Pre-stream failures (validation, auth) reject with
// an Error; mid-stream failures surface through cb.onError.
export async function streamChat(opts: ChatOptions, cb: ChatStreamCallbacks): Promise<void> {
  const res = await fetch("/api/v1/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
    body: JSON.stringify({
      session_id: opts.sessionId,
      message: opts.message,
      model: opts.model,
      provider_id: opts.providerId,
    }),
    credentials: "include",
    signal: opts.signal,
  });

  if (!res.ok || !res.body) {
    throw new Error(await parseError(res));
  }

  await consumeChatStream(res, cb);
}

// Records an approve/deny decision for a pending tool call. The response is an
// SSE stream: the decision event first, then the resumed run when every call
// of the turn has been decided.
export async function decideApproval(
  id: number,
  decision: "approve" | "deny",
  cb: ChatStreamCallbacks,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/v1/approvals", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
    body: JSON.stringify({ id, decision }),
    credentials: "include",
    signal,
  });

  if (!res.ok || !res.body) {
    throw new Error(await parseError(res));
  }

  await consumeChatStream(res, cb);
}

async function consumeChatStream(res: Response, cb: ChatStreamCallbacks): Promise<void> {
  if (!res.body) return;

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
      case "tool_calls": {
        const calls = Array.isArray(payload.calls) ? (payload.calls as ToolCallInfo[]) : [];
        cb.onToolCalls?.(calls);
        break;
      }
      case "tool_result":
        cb.onToolResult?.(
          typeof payload.tool_call_id === "string" ? payload.tool_call_id : "",
          typeof payload.name === "string" ? payload.name : "",
          typeof payload.result === "string" ? payload.result : "",
        );
        break;
      case "approval_required":
        cb.onApprovalRequired?.({
          id: Number(payload.id),
          toolCallId: typeof payload.tool_call_id === "string" ? payload.tool_call_id : "",
          messageId: Number(payload.message_id),
          name: typeof payload.name === "string" ? payload.name : "",
          arguments: typeof payload.arguments === "string" ? payload.arguments : "",
        });
        break;
      case "decision":
        cb.onDecision?.(
          Number(payload.id),
          typeof payload.result === "string" ? payload.result : "",
          typeof payload.status === "string" ? payload.status : "",
        );
        break;
    }
  }
}

export interface Job {
  id: number;
  session_id: number | null;
  schedule: string;
  prompt: string;
  enabled: boolean;
  provider_id: number | null;
  model: string;
  last_run_at: string | null;
  last_status: string;
  next_run_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface JobInput {
  schedule: string;
  prompt: string;
  session_id?: number;
  provider_id?: number;
  model?: string;
}

export async function listJobs(): Promise<Job[]> {
  const res = await fetch("/api/v1/jobs", { credentials: "include" });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return (await res.json()) as Job[];
}

export async function createJob(input: JobInput): Promise<Job> {
  const res = await postJSON("/api/v1/jobs", input);
  return (await res.json()) as Job;
}

export async function updateJob(
  id: number,
  patch: { schedule?: string; prompt?: string; enabled?: boolean; provider_id?: number; model?: string },
): Promise<Job> {
  const res = await patchJSON(`/api/v1/jobs/${id}`, patch);
  return (await res.json()) as Job;
}

export async function deleteJob(id: number): Promise<void> {
  await deleteRequest(`/api/v1/jobs/${id}`);
}

export interface SessionInfo {
  id: number;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface SessionToolCall {
  id: number;
  call_id: string;
  name: string;
  arguments: string;
  result: string;
  status: string;
}

export interface SessionMessage {
  id: number;
  role: string;
  content: string;
  created_at: string;
  tool_calls?: SessionToolCall[];
}

export async function listSessions(): Promise<SessionInfo[]> {
  const res = await getRequest("/api/v1/sessions");
  return (await res.json()) as SessionInfo[];
}

export async function listSessionMessages(id: number): Promise<SessionMessage[]> {
  const res = await getRequest(`/api/v1/sessions/${id}/messages`);
  return (await res.json()) as SessionMessage[];
}

export async function renameSession(id: number, title: string): Promise<SessionInfo> {
  const res = await patchJSON(`/api/v1/sessions/${id}`, { title });
  return (await res.json()) as SessionInfo;
}

export async function deleteSession(id: number): Promise<void> {
  await deleteRequest(`/api/v1/sessions/${id}`);
}

export interface LLMProvider {
  id: number;
  name: string;
  provider: string;
  base_url: string;
  has_api_key: boolean;
  models: string[];
  default_model: string;
  default: boolean;
}

export interface LLMProviderInput {
  name: string;
  provider: string;
  base_url: string;
  api_key?: string;
  models: string[];
  default_model?: string;
  default?: boolean;
}

export async function listLLMProviders(): Promise<LLMProvider[]> {
  const res = await getRequest("/api/v1/llm/providers");
  return (await res.json()) as LLMProvider[];
}

export async function createLLMProvider(input: LLMProviderInput): Promise<LLMProvider> {
  const res = await postJSON("/api/v1/llm/providers", input);
  return (await res.json()) as LLMProvider;
}

export async function updateLLMProvider(id: number, input: LLMProviderInput): Promise<LLMProvider> {
  const res = await patchJSON(`/api/v1/llm/providers/${id}`, input);
  return (await res.json()) as LLMProvider;
}

export async function deleteLLMProvider(id: number): Promise<void> {
  await deleteRequest(`/api/v1/llm/providers/${id}`);
}

// Asks the gateway for its available models (admin-only).
export async function fetchProviderModels(id: number): Promise<string[]> {
  const res = await fetch(`/api/v1/llm/providers/${id}/models`, {
    method: "POST",
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  const body = (await res.json()) as { models: string[] };
  return body.models;
}

export interface LLMPrefs {
  provider_id: number | null;
  model: string;
}

export async function getLLMPrefs(): Promise<LLMPrefs> {
  const res = await getRequest("/api/v1/llm/prefs");
  return (await res.json()) as LLMPrefs;
}

export async function setLLMPrefs(providerId: number, model?: string): Promise<LLMPrefs> {
  const res = await fetch("/api/v1/llm/prefs", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider_id: providerId, model: model ?? "" }),
    credentials: "include",
  });
  if (!res.ok) {
    throw new Error(await parseError(res));
  }
  return (await res.json()) as LLMPrefs;
}

export async function clearLLMPrefs(): Promise<LLMPrefs> {
  return setLLMPrefs(0);
}

export interface AdminUser {
  id: number;
  email: string;
  role: string;
  created_at: string;
}

export async function listUsers(): Promise<AdminUser[]> {
  const res = await getRequest("/api/v1/users");
  return (await res.json()) as AdminUser[];
}

export async function createUser(email: string, password: string): Promise<AdminUser> {
  const res = await postJSON("/api/v1/users", { email, password });
  return (await res.json()) as AdminUser;
}

export async function deleteUser(id: number): Promise<void> {
  await deleteRequest(`/api/v1/users/${id}`);
}
