<script lang="ts">
  import { onMount } from "svelte";
  import { getSettings, saveSettings, type LLMSettings } from "$lib/api";

  let provider = $state("");
  let baseUrl = $state("");
  let apiKey = $state("");
  let model = $state("");
  let hasApiKey = $state(false);

  let loading = $state(true);
  let saving = $state(false);
  let error = $state("");
  let notice = $state("");

  let custom = $derived(provider === "openai");
  let canSave = $derived(!custom || (baseUrl.trim() !== "" && model.trim() !== "" && (apiKey !== "" || hasApiKey)));

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    error = "";
    try {
      const s = await getSettings();
      apply(s);
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to load settings";
    } finally {
      loading = false;
    }
  }

  function apply(s: LLMSettings) {
    provider = s.provider;
    baseUrl = s.base_url;
    model = s.model;
    hasApiKey = s.has_api_key;
    apiKey = "";
  }

  async function submit() {
    if (saving || !canSave) return;
    saving = true;
    error = "";
    notice = "";
    try {
      const saved = await saveSettings({
        provider,
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
        model: model.trim(),
      });
      apply(saved);
      notice = provider === "" ? "Using the server default provider." : "Settings saved.";
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to save settings";
    } finally {
      saving = false;
    }
  }
</script>

<div class="settings">
  <div class="column">
    <div class="toolbar">
      <h1>Settings</h1>
    </div>

    {#if loading}
      <div class="empty">Loading...</div>
    {:else}
      <form
        class="form"
        onsubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <h2>LLM provider</h2>
        <p class="hint">
          Choose your own OpenAI-compatible provider, or leave it on the server default. Scheduled jobs run with these
          settings too.
        </p>

        <div class="field">
          <label>
            Provider
            <select bind:value={provider}>
              <option value="">Server default</option>
              <option value="openai">OpenAI-compatible</option>
            </select>
          </label>
        </div>

        {#if custom}
          <div class="field">
            <label>
              Base URL
              <input type="url" bind:value={baseUrl} placeholder="https://api.openai.com/v1" />
            </label>
          </div>

          <div class="field">
            <label>
              API key
              <input
                type="password"
                bind:value={apiKey}
                placeholder={hasApiKey ? "Stored (leave blank to keep)" : "sk-..."}
                autocomplete="off"
              />
            </label>
          </div>

          <div class="field">
            <label>
              Model
              <input type="text" bind:value={model} placeholder="gpt-4o" />
            </label>
          </div>
        {/if}

        <div class="actions">
          <button class="primary" type="submit" disabled={saving || !canSave}>Save</button>
        </div>
      </form>

      {#if error}
        <div class="error">{error}</div>
      {/if}
      {#if notice}
        <div class="notice">{notice}</div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .settings {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .column {
    max-width: 760px;
    margin: 0 auto;
    padding: 24px 20px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  h1 {
    font-size: 20px;
    margin: 0;
  }

  h2 {
    font-size: 15px;
    margin: 0;
  }

  .hint {
    color: var(--text-dim);
    font-size: 13px;
    margin: 0;
  }

  .form {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .field label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    color: var(--text-dim);
  }

  .field input,
  .field select {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 10px 12px;
    outline: none;
    color: var(--text);
  }

  .field input:focus,
  .field select:focus {
    border-color: var(--accent);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
  }

  button.primary {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius);
    padding: 10px 16px;
    font-weight: 600;
  }

  button.primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .error {
    color: var(--danger);
    font-size: 13px;
  }

  .notice {
    color: var(--text-dim);
    font-size: 13px;
  }

  .empty {
    color: var(--text-dim);
    text-align: center;
    padding: 40px 0;
  }
</style>
