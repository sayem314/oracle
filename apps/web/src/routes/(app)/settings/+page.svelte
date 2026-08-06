<script lang="ts">
  import { onMount } from "svelte";
  import {
    listLLMProviders,
    createLLMProvider,
    updateLLMProvider,
    deleteLLMProvider,
    type LLMProvider,
  } from "$lib/api";

  let { data } = $props<{ data: import("./$types").PageData }>();
  let isAdmin = $derived(data.user?.role === "admin");

  let providers = $state<LLMProvider[]>([]);
  let loading = $state(true);
  let saving = $state(false);
  let error = $state("");
  let notice = $state("");

  let editingId = $state<number | null>(null);
  let name = $state("");
  let baseUrl = $state("");
  let apiKey = $state("");
  let modelsText = $state("");
  let defaultModel = $state("");

  let models = $derived(
    modelsText
      .split(",")
      .map((m) => m.trim())
      .filter((m, i, all) => m !== "" && all.indexOf(m) === i),
  );
  let hasApiKey = $derived(providers.find((p) => p.id === editingId)?.has_api_key ?? false);
  let canSave = $derived(
    name.trim() !== "" && baseUrl.trim() !== "" && (editingId === null || apiKey !== "" || hasApiKey),
  );

  onMount(() => {
    if (isAdmin) void refresh();
  });

  async function refresh() {
    error = "";
    try {
      providers = await listLLMProviders();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to load providers";
    } finally {
      loading = false;
    }
  }

  function startAdd() {
    editingId = null;
    name = "";
    baseUrl = "";
    apiKey = "";
    modelsText = "";
    defaultModel = "";
    error = "";
    notice = "";
  }

  function startEdit(p: LLMProvider) {
    editingId = p.id;
    name = p.name;
    baseUrl = p.base_url;
    apiKey = "";
    modelsText = p.models.join(", ");
    defaultModel = p.default_model;
    error = "";
    notice = "";
  }

  async function submit() {
    if (saving || !canSave) return;
    saving = true;
    error = "";
    notice = "";
    const payload = {
      name: name.trim(),
      provider: "openai",
      base_url: baseUrl.trim(),
      api_key: apiKey.trim(),
      models,
      default_model: defaultModel || undefined,
    };
    try {
      if (editingId === null) {
        await createLLMProvider(payload);
      } else {
        await updateLLMProvider(editingId, payload);
      }
      notice = editingId === null ? "Provider added." : "Provider updated.";
      startAdd();
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to save provider";
    } finally {
      saving = false;
    }
  }

  async function makeDefault(p: LLMProvider) {
    error = "";
    notice = "";
    try {
      await updateLLMProvider(p.id, {
        name: p.name,
        provider: p.provider,
        base_url: p.base_url,
        models: p.models,
        default_model: p.default_model,
        default: true,
      });
      notice = `${p.name} is now the default.`;
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to set default";
    }
  }

  async function remove(p: LLMProvider) {
    error = "";
    notice = "";
    try {
      await deleteLLMProvider(p.id);
      if (editingId === p.id) startAdd();
      notice = `Deleted ${p.name}.`;
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to delete provider";
    }
  }
</script>

<div class="settings">
  {#if !isAdmin}
    <div class="empty">Admin access required to manage providers.</div>
  {:else}
    <div class="column">
      <div class="toolbar">
        <h1>Settings</h1>
        <button class="primary" type="button" onclick={startAdd}>Add provider</button>
      </div>

      {#if error}
        <div class="error">{error}</div>
      {/if}
      {#if notice}
        <div class="notice">{notice}</div>
      {/if}

      {#if editingId !== null || (loading && providers.length === 0)}
        <form
          class="form"
          onsubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <h2>{editingId === null ? "New provider" : "Edit provider"}</h2>
          <div class="field">
            <label>
              Name
              <input type="text" bind:value={name} placeholder="e.g. OpenRouter, Ollama" />
            </label>
          </div>
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
                placeholder={editingId !== null && hasApiKey ? "Stored (leave blank to keep)" : "sk-..."}
                autocomplete="off"
              />
            </label>
          </div>
          <div class="field">
            <label>
              Models (comma-separated)
              <input type="text" bind:value={modelsText} placeholder="gpt-4o, gpt-4o-mini" />
            </label>
          </div>
          {#if models.length > 0}
            <div class="field">
              <label>
                Default model
                <select bind:value={defaultModel}>
                  <option value="">None (choose per message)</option>
                  {#each models as m (m)}
                    <option value={m}>{m}</option>
                  {/each}
                </select>
              </label>
            </div>
          {/if}
          <div class="actions">
            <button type="button" class="ghost" onclick={startAdd}>Cancel</button>
            <button class="primary" type="submit" disabled={saving || !canSave}>Save</button>
          </div>
        </form>
      {/if}

      {#if loading}
        <div class="empty">Loading...</div>
      {:else if providers.length === 0 && editingId === null}
        <div class="empty">No providers yet. Add one to use your own LLM, or chat with the server default.</div>
      {:else}
        <div class="list">
          {#each providers as p (p.id)}
            <div class="provider">
              <div class="provider-main">
                <div class="provider-head">
                  <span class="provider-name">{p.name}</span>
                  {#if p.default}
                    <span class="badge default">default</span>
                  {/if}
                  <span class="provider-meta">{p.base_url}</span>
                </div>
                {#if p.models.length > 0}
                  <div class="models">
                    {#each p.models as m (m)}
                      <span class="model" class:default-model={m === p.default_model}>
                        {m}
                        {#if m === p.default_model}•{/if}
                      </span>
                    {/each}
                  </div>
                {:else}
                  <div class="provider-meta">No models configured</div>
                {/if}
              </div>
              <div class="provider-actions">
                {#if !p.default}
                  <button class="ghost" onclick={() => void makeDefault(p)}>Make default</button>
                {/if}
                <button class="ghost" onclick={() => startEdit(p)}>Edit</button>
                <button class="delete" onclick={() => void remove(p)}>Delete</button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
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
    gap: 10px;
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

  button.ghost {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text);
    padding: 6px 12px;
    font-size: 13px;
  }

  button.ghost:hover {
    border-color: var(--accent);
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .provider {
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 12px 14px;
  }

  .provider-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .provider-head {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  .provider-name {
    font-weight: 600;
  }

  .badge {
    font-size: 12px;
    padding: 2px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    color: var(--text-dim);
  }

  .badge.default {
    color: var(--accent);
    border-color: var(--accent);
  }

  .provider-meta {
    font-size: 12px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .models {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }

  .model {
    font-size: 12px;
    padding: 2px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    color: var(--text-dim);
  }

  .model.default-model {
    color: var(--accent);
    border-color: var(--accent);
  }

  .provider-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  button.delete {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-input);
    color: var(--danger);
    padding: 6px 12px;
    font-size: 13px;
  }

  button.delete:hover {
    border-color: var(--danger);
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
