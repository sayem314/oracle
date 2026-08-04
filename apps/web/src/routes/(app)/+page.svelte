<script lang="ts">
  import { onMount, tick } from "svelte";
  import {
    streamChat,
    decideApproval,
    listSessions,
    listSessionMessages,
    renameSession,
    deleteSession,
    type ChatStreamCallbacks,
    type SessionInfo,
  } from "$lib/api";

  type ToolStatus = "running" | "awaiting" | "done" | "denied";
  type ToolBlock = {
    kind: "tool";
    rowId: number | null;
    callId: string;
    name: string;
    result: string;
    status: ToolStatus;
  };

  type Block = { kind: "user"; content: string } | { kind: "assistant"; content: string } | ToolBlock;

  let blocks = $state<Block[]>([]);
  let sessions = $state<SessionInfo[]>([]);
  let activeSessionId = $state<number | null>(null);

  let input = $state("");
  let streaming = $state(false);
  let streamingContent = $state("");
  let error = $state("");
  let loadingHistory = $state(false);

  let renamingId = $state<number | null>(null);
  let renameValue = $state("");

  let scroller: HTMLElement | undefined = $state();
  let field: HTMLTextAreaElement | undefined = $state();
  let aborter: AbortController | null = null;

  let awaitingApproval = $derived(blocks.some((b) => b.kind === "tool" && b.status === "awaiting"));

  onMount(() => {
    void refreshSessions();
  });

  async function scrollToBottom() {
    await tick();
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  }

  async function refreshSessions() {
    try {
      sessions = await listSessions();
    } catch {
      // A failed sidebar load should not block chatting.
    }
  }

  function patchTool(match: (b: ToolBlock) => boolean, patch: Partial<ToolBlock>) {
    blocks = blocks.map((b) => {
      if (b.kind !== "tool") return b;
      return match(b) ? { ...b, ...patch } : b;
    });
  }

  async function newChat() {
    activeSessionId = null;
    blocks = [];
    streamingContent = "";
    error = "";
    field?.focus();
  }

  async function openSession(id: number) {
    if (streaming) return;
    activeSessionId = id;
    error = "";
    loadingHistory = true;
    try {
      const msgs = await listSessionMessages(id);
      const next: Block[] = [];
      for (const m of msgs) {
        if (m.role === "user") {
          next.push({ kind: "user", content: m.content });
        } else if (m.role === "assistant") {
          if (m.content) next.push({ kind: "assistant", content: m.content });
          for (const tc of m.tool_calls ?? []) {
            next.push({
              kind: "tool",
              rowId: tc.id,
              callId: tc.call_id,
              name: tc.name,
              result: tc.result,
              status: toolStatusFrom(tc.status),
            });
          }
        }
      }
      blocks = next;
      await scrollToBottom();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to load session";
    } finally {
      loadingHistory = false;
    }
  }

  function toolStatusFrom(status: string): ToolStatus {
    switch (status) {
      case "done":
        return "done";
      case "denied":
        return "denied";
      case "awaiting_approval":
        return "awaiting";
      default:
        return "done";
    }
  }

  async function startRename(s: SessionInfo) {
    renamingId = s.id;
    renameValue = s.title;
  }

  async function commitRename() {
    if (renamingId === null) return;
    const id = renamingId;
    renamingId = null;
    try {
      await renameSession(id, renameValue.trim());
      await refreshSessions();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to rename session";
    }
  }

  async function removeSession(id: number) {
    try {
      await deleteSession(id);
      if (activeSessionId === id) {
        await newChat();
      }
      await refreshSessions();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to delete session";
    }
  }

  function makeCallbacks(finalized: () => void): ChatStreamCallbacks {
    return {
      onStart: (sid) => {
        activeSessionId = sid;
        void refreshSessions();
      },
      onDelta: (content) => {
        streamingContent += content;
        scrollToBottom();
      },
      onToolCalls: (calls) => {
        blocks = [
          ...blocks,
          ...calls.map((c) => ({
            kind: "tool" as const,
            rowId: null,
            callId: c.id,
            name: c.name,
            result: "",
            status: "running" as const,
          })),
        ];
        scrollToBottom();
      },
      onToolResult: (toolCallId, _name, result) => {
        patchTool((b) => b.callId === toolCallId, { result, status: "done" });
        scrollToBottom();
      },
      onApprovalRequired: (approval) => {
        patchTool((b) => b.callId === approval.toolCallId, { rowId: approval.id, status: "awaiting" });
        scrollToBottom();
      },
      onDecision: (id, result, status) => {
        patchTool((b) => b.rowId === id, { result, status: status === "denied" ? "denied" : "done" });
        scrollToBottom();
      },
      onDone: (_messageId, finishReason) => {
        finalized();
        if (finishReason === "awaiting_approval" && !streamingContent) {
          resetStream();
        } else {
          commit();
        }
        void refreshSessions();
      },
      onError: (msg) => {
        finalized();
        error = msg;
        resetStream();
      },
    };
  }

  async function run(request: (cb: ChatStreamCallbacks) => Promise<void>) {
    error = "";
    streaming = true;
    streamingContent = "";
    scrollToBottom();

    aborter = new AbortController();
    let finalized = false;
    const cb = makeCallbacks(() => {
      finalized = true;
    });
    try {
      await request(cb);
      if (!finalized && streaming) {
        commit();
      }
    } catch (err) {
      error = err instanceof Error ? err.message : "request failed";
      resetStream();
    } finally {
      aborter = null;
      field?.focus();
    }
  }

  async function send() {
    const message = input.trim();
    if (!message || streaming || awaitingApproval) return;

    input = "";
    blocks = [...blocks, { kind: "user", content: message }];
    await run((cb) => streamChat({ sessionId: activeSessionId, message, signal: aborter?.signal }, cb));
  }

  async function decide(rowId: number, decision: "approve" | "deny") {
    if (streaming) return;
    await run((cb) => decideApproval(rowId, decision, cb));
  }

  function commit() {
    if (streamingContent) {
      blocks = [...blocks, { kind: "assistant", content: streamingContent }];
    }
    resetStream();
    scrollToBottom();
  }

  function resetStream() {
    streaming = false;
    streamingContent = "";
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void send();
    }
  }

  function titleOf(s: SessionInfo): string {
    return s.title.trim() || `Session ${s.id}`;
  }
</script>

<div class="chat-layout">
  <aside class="sidebar">
    <button class="new-chat" onclick={() => void newChat()}>New chat</button>
    <div class="session-list">
      {#each sessions as s (s.id)}
        <div class="session" class:active={s.id === activeSessionId}>
          {#if renamingId === s.id}
            <input
              class="rename-input"
              bind:value={renameValue}
              onkeydown={(e) => {
                if (e.key === "Enter") void commitRename();
                if (e.key === "Escape") renamingId = null;
              }}
              onblur={() => void commitRename()}
            />
          {:else}
            <button class="session-open" onclick={() => void openSession(s.id)}>{titleOf(s)}</button>
            <span class="session-actions">
              <button class="icon" title="Rename" onclick={() => void startRename(s)}>&#9998;</button>
              <button class="icon danger" title="Delete" onclick={() => void removeSession(s.id)}>&#10005;</button>
            </span>
          {/if}
        </div>
      {/each}
      {#if sessions.length === 0}
        <div class="no-sessions">No conversations yet.</div>
      {/if}
    </div>
  </aside>

  <div class="chat">
    <div class="scroller" bind:this={scroller}>
      <div class="column">
        {#if loadingHistory}
          <div class="empty"><p>Loading...</p></div>
        {:else if blocks.length === 0 && !streaming}
          <div class="empty">
            <p>Ask oracle anything to get started.</p>
          </div>
        {/if}

        {#each blocks as block}
          {#if block.kind === "user"}
            <div class="msg user">
              <div class="bubble">{block.content}</div>
            </div>
          {:else if block.kind === "assistant"}
            <div class="msg assistant">
              <div class="bubble">{block.content}</div>
            </div>
          {:else}
            <div class="tool {block.status}">
              <span class="tool-name">{block.name}</span>
              {#if block.status === "awaiting"}
                <span class="tool-status">needs approval</span>
                <span class="tool-actions">
                  <button
                    class="approve"
                    disabled={streaming}
                    onclick={() => block.rowId !== null && void decide(block.rowId, "approve")}
                  >
                    Approve
                  </button>
                  <button
                    class="deny"
                    disabled={streaming}
                    onclick={() => block.rowId !== null && void decide(block.rowId, "deny")}>Deny</button
                  >
                </span>
              {:else if block.status === "running"}
                <span class="tool-status">running...</span>
              {:else if block.result}
                <span class="tool-result">{block.result}</span>
              {/if}
            </div>
          {/if}
        {/each}

        {#if streaming}
          <div class="msg assistant">
            <div class="bubble">
              {#if streamingContent}
                {streamingContent}
              {:else}
                <span class="thinking">thinking...</span>
              {/if}
            </div>
          </div>
        {/if}

        {#if error}
          <div class="error">{error}</div>
        {/if}
      </div>
    </div>

    <div class="composer-bar">
      <form
        class="composer"
        onsubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <textarea
          bind:this={field}
          bind:value={input}
          onkeydown={onKeydown}
          rows="1"
          placeholder={awaitingApproval ? "Approve or deny the pending tool call" : "Message oracle"}
          disabled={streaming || awaitingApproval}></textarea>
        <button type="submit" class="send" disabled={streaming || awaitingApproval || !input.trim()}>Send</button>
      </form>
    </div>
  </div>
</div>

<style>
  .chat-layout {
    display: flex;
    height: 100%;
    min-height: 0;
  }

  .sidebar {
    width: 240px;
    flex-shrink: 0;
    border-right: 1px solid var(--border);
    background: var(--bg-raised);
    display: flex;
    flex-direction: column;
    padding: 12px;
    gap: 12px;
    overflow-y: auto;
  }

  .new-chat {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius);
    padding: 8px 12px;
    font-weight: 600;
  }

  .session-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .session {
    display: flex;
    align-items: center;
    gap: 4px;
    border-radius: 8px;
    padding: 2px 4px;
  }

  .session.active {
    background: var(--bg-input);
  }

  .session-open {
    flex: 1;
    min-width: 0;
    text-align: left;
    background: transparent;
    border: none;
    color: var(--text);
    padding: 6px 8px;
    border-radius: 6px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 14px;
  }

  .session-open:hover {
    background: var(--bg-input);
  }

  .session-actions {
    display: flex;
    gap: 2px;
    opacity: 0;
    transition: opacity 0.12s;
  }

  .session:hover .session-actions {
    opacity: 1;
  }

  .icon {
    background: transparent;
    border: none;
    color: var(--text-dim);
    padding: 4px 6px;
    border-radius: 6px;
    font-size: 12px;
  }

  .icon:hover {
    color: var(--text);
    background: var(--bg);
  }

  .icon.danger:hover {
    color: var(--danger);
  }

  .rename-input {
    flex: 1;
    min-width: 0;
    background: var(--bg-input);
    border: 1px solid var(--accent);
    border-radius: 6px;
    padding: 6px 8px;
    font-size: 14px;
    outline: none;
  }

  .no-sessions {
    color: var(--text-dim);
    font-size: 13px;
    padding: 8px;
  }

  .chat {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    flex: 1;
  }

  .scroller {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .column {
    max-width: 760px;
    margin: 0 auto;
    padding: 24px 20px 12px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .empty {
    color: var(--text-dim);
    text-align: center;
    padding: 60px 0;
  }

  .msg {
    display: flex;
  }

  .msg.user {
    justify-content: flex-end;
  }

  .msg.assistant {
    justify-content: flex-start;
  }

  .bubble {
    max-width: 78%;
    padding: 10px 14px;
    border-radius: 14px;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .msg.user .bubble {
    background: var(--accent);
    color: #fff;
    border-bottom-right-radius: 4px;
  }

  .msg.assistant .bubble {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-bottom-left-radius: 4px;
  }

  .thinking {
    color: var(--text-dim);
    font-style: italic;
  }

  .tool {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--bg-raised);
    font-size: 13px;
  }

  .tool-name {
    font-weight: 600;
  }

  .tool-status {
    color: var(--text-dim);
    font-style: italic;
  }

  .tool.awaiting {
    border-color: var(--accent);
  }

  .tool.denied .tool-result {
    color: var(--danger);
  }

  .tool-result {
    color: var(--text-dim);
    white-space: pre-wrap;
    word-break: break-word;
  }

  .tool-actions {
    display: flex;
    gap: 6px;
    margin-left: auto;
  }

  .tool-actions button {
    border: none;
    border-radius: 8px;
    padding: 5px 12px;
    font-weight: 600;
    cursor: pointer;
  }

  .tool-actions button.approve {
    background: var(--accent);
    color: #fff;
  }

  .tool-actions button.deny {
    background: var(--bg-input);
    border: 1px solid var(--border);
    color: inherit;
  }

  .tool-actions button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .error {
    color: var(--danger);
    font-size: 13px;
    text-align: center;
  }

  .composer-bar {
    padding: 14px 20px 18px;
    border-top: 1px solid var(--border);
    background: var(--bg-raised);
  }

  .composer {
    display: flex;
    gap: 10px;
    align-items: flex-end;
    max-width: 760px;
    margin: 0 auto;
  }

  textarea {
    flex: 1;
    resize: none;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 10px 12px;
    outline: none;
    max-height: 160px;
  }

  textarea:focus {
    border-color: var(--accent);
  }

  button.send {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: 10px;
    padding: 10px 18px;
    font-weight: 600;
  }

  button.send:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
