<script lang="ts">
  import { tick } from "svelte";
  import { streamChat } from "$lib/api";

  interface Message {
    role: "user" | "assistant";
    content: string;
  }

  let messages = $state<Message[]>([]);
  let input = $state("");
  let sessionId = $state<number | null>(null);
  let streaming = $state(false);
  let streamingContent = $state("");
  let error = $state("");

  let scroller: HTMLElement | undefined = $state();
  let field: HTMLTextAreaElement | undefined = $state();
  let aborter: AbortController | null = null;

  async function scrollToBottom() {
    await tick();
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  }

  async function send() {
    const message = input.trim();
    if (!message || streaming) return;

    error = "";
    input = "";
    messages = [...messages, { role: "user", content: message }];
    streaming = true;
    streamingContent = "";
    scrollToBottom();

    aborter = new AbortController();
    let finalized = false;
    try {
      await streamChat(
        { sessionId, message, signal: aborter.signal },
        {
          onStart: (sid) => {
            sessionId = sid;
          },
          onDelta: (content) => {
            streamingContent += content;
            scrollToBottom();
          },
          onDone: () => {
            finalized = true;
            commit();
          },
          onError: (msg) => {
            finalized = true;
            error = msg;
            resetStream();
          },
        },
      );
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

  function commit() {
    messages = [...messages, { role: "assistant", content: streamingContent }];
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
      {#if messages.length === 0 && !streaming}
        <div class="empty">
          <p>Ask oracle anything to get started.</p>
        </div>
      {/if}

      {#each messages as m}
        <div class="msg {m.role}">
          <div class="bubble">{m.content}</div>
        </div>
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
        placeholder="Message oracle"
        disabled={streaming}></textarea>
      <button type="submit" class="send" disabled={streaming || !input.trim()}>Send</button>
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
