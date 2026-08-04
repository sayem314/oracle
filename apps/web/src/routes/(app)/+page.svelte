<script lang="ts">
  import { tick } from "svelte";
  import { streamChat, decideApproval, type ChatStreamCallbacks } from "$lib/api";

  interface Message {
    role: "user" | "assistant";
    content: string;
  }

  interface ToolCard {
    rowId: number | null;
    callId: string;
    name: string;
    result: string;
    status: "running" | "awaiting" | "done" | "denied";
  }

  let messages = $state<Message[]>([]);
  let toolCards = $state<ToolCard[]>([]);
  let input = $state("");
  let sessionId = $state<number | null>(null);
  let streaming = $state(false);
  let streamingContent = $state("");
  let error = $state("");

  let scroller: HTMLElement | undefined = $state();
  let field: HTMLTextAreaElement | undefined = $state();
  let aborter: AbortController | null = null;

  let awaitingApproval = $derived(toolCards.some((c) => c.status === "awaiting"));

  async function scrollToBottom() {
    await tick();
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  }

  function updateCard(match: (c: ToolCard) => boolean, patch: Partial<ToolCard>) {
    toolCards = toolCards.map((c) => (match(c) ? { ...c, ...patch } : c));
  }

  function makeCallbacks(finalized: () => void): ChatStreamCallbacks {
    return {
      onStart: (sid) => {
        sessionId = sid;
      },
      onDelta: (content) => {
        streamingContent += content;
        scrollToBottom();
      },
      onToolCalls: (calls) => {
        toolCards = [
          ...toolCards,
          ...calls.map((c) => ({ rowId: null, callId: c.id, name: c.name, result: "", status: "running" as const })),
        ];
        scrollToBottom();
      },
      onToolResult: (toolCallId, _name, result) => {
        updateCard((c) => c.callId === toolCallId, { result, status: "done" });
        scrollToBottom();
      },
      onApprovalRequired: (approval) => {
        updateCard((c) => c.callId === approval.toolCallId, { rowId: approval.id, status: "awaiting" });
        scrollToBottom();
      },
      onDecision: (id, result, status) => {
        updateCard((c) => c.rowId === id, { result, status: status === "denied" ? "denied" : "done" });
        scrollToBottom();
      },
      onDone: (_messageId, finishReason) => {
        finalized();
        if (finishReason === "awaiting_approval" && !streamingContent) {
          resetStream();
        } else {
          commit();
        }
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
    messages = [...messages, { role: "user", content: message }];
    await run((cb) => streamChat({ sessionId, message, signal: aborter?.signal }, cb));
  }

  async function decide(card: ToolCard, decision: "approve" | "deny") {
    if (card.rowId === null || streaming) return;
    const rowId = card.rowId;
    await run((cb) => decideApproval(rowId, decision, cb));
  }

  function commit() {
    if (streamingContent) {
      messages = [...messages, { role: "assistant", content: streamingContent }];
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
</script>

<div class="chat">
  <div class="scroller" bind:this={scroller}>
    <div class="column">
      {#if messages.length === 0 && !streaming && toolCards.length === 0}
        <div class="empty">
          <p>Ask oracle anything to get started.</p>
        </div>
      {/if}

      {#each messages as m}
        <div class="msg {m.role}">
          <div class="bubble">{m.content}</div>
        </div>
      {/each}

      {#if toolCards.length > 0}
        <div class="tools">
          {#each toolCards as card}
            <div class="tool {card.status}">
              <span class="tool-name">{card.name}</span>
              {#if card.status === "awaiting"}
                <span class="tool-status">needs approval</span>
                <span class="tool-actions">
                  <button class="approve" disabled={streaming} onclick={() => decide(card, "approve")}>Approve</button>
                  <button class="deny" disabled={streaming} onclick={() => decide(card, "deny")}>Deny</button>
                </span>
              {:else if card.status === "running"}
                <span class="tool-status">running...</span>
              {:else if card.result}
                <span class="tool-result">{card.result}</span>
              {/if}
            </div>
          {/each}
        </div>
      {/if}

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

<style>
  .chat {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
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

  .tools {
    display: flex;
    flex-direction: column;
    gap: 6px;
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
