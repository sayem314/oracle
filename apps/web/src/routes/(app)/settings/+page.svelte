<script lang="ts">
  import { onMount } from "svelte";
  import {
    getLLMProvider,
    updateLLMProvider,
    fetchLLMModels,
    getSettings,
    updateSettings,
    changePassword,
    type LLMProvider,
  } from "$lib/api";

  let { data } = $props<{ data: import("./$types").PageData }>();
  let isAdmin = $derived(data.user?.role === "admin");

  let curPw = $state("");
  let newPw = $state("");
  let confirmPw = $state("");
  let revokeOtherSessions = $state(true);
  let pwSaving = $state(false);
  let pwError = $state("");
  let pwNotice = $state("");
  let pwValid = $derived(curPw !== "" && newPw.length >= 8 && newPw === confirmPw);

  let provider = $state<LLMProvider | null>(null);
  let baseUrl = $state("");
  let apiKey = $state("");
  let model = $state("");
  let providerSaving = $state(false);
  let hasApiKey = $state(false);
  let fetching = $state(false);
  let pError = $state("");
  let pNotice = $state("");

  let permissionDefault = $state("ask");
  let permissionRules = $state("");
  let settingsSaving = $state(false);
  let sError = $state("");
  let sNotice = $state("");

  let loading = $state(true);
  let error = $state("");

  onMount(() => {
    if (isAdmin) void refresh();
  });

  async function refresh() {
    error = "";
    loading = true;
    try {
      const [p, s] = await Promise.all([getLLMProvider(), getSettings()]);
      provider = p;
      baseUrl = p.base_url;
      model = p.model;
      hasApiKey = p.has_api_key;
      permissionDefault = s.permission_default;
      permissionRules = s.permission_rules;
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to load settings";
    } finally {
      loading = false;
    }
  }

  async function fetchModels() {
    if (fetching) return;
    fetching = true;
    pError = "";
    pNotice = "";
    try {
      const fetched = await fetchLLMModels();
      pNotice =
        fetched.length > 0 ? `Fetched ${fetched.length} models from the gateway.` : "The gateway returned no models.";
    } catch (err) {
      pError = err instanceof Error ? err.message : "failed to fetch models";
    } finally {
      fetching = false;
    }
  }

  async function saveProvider() {
    if (providerSaving) return;
    providerSaving = true;
    pError = "";
    pNotice = "";
    try {
      const updated = await updateLLMProvider({
        base_url: baseUrl.trim(),
        api_key: apiKey.trim() || undefined,
        model: model.trim() || undefined,
      });
      hasApiKey = updated.has_api_key;
      apiKey = "";
      pNotice = "Provider updated.";
    } catch (err) {
      pError = err instanceof Error ? err.message : "failed to save provider";
    } finally {
      providerSaving = false;
    }
  }

  async function saveSettings() {
    if (settingsSaving) return;
    settingsSaving = true;
    sError = "";
    sNotice = "";
    try {
      const updated = await updateSettings({
        permission_default: permissionDefault,
        permission_rules: permissionRules,
      });
      permissionDefault = updated.permission_default;
      permissionRules = updated.permission_rules;
      sNotice = "Ruleset updated.";
    } catch (err) {
      sError = err instanceof Error ? err.message : "failed to save ruleset";
    } finally {
      settingsSaving = false;
    }
  }

  async function changePw() {
    if (pwSaving || !pwValid) return;
    pwSaving = true;
    pwError = "";
    pwNotice = "";
    try {
      await changePassword(curPw, newPw, revokeOtherSessions);
      curPw = "";
      newPw = "";
      confirmPw = "";
      pwNotice = "Password updated.";
    } catch (err) {
      pwError = err instanceof Error ? err.message : "failed to change password";
    } finally {
      pwSaving = false;
    }
  }
</script>

<div class="settings">
  <div class="column">
    {#if !isAdmin}
      <div class="empty">Admin access required to manage settings.</div>
    {:else}
      {#if error}
        <div class="error">{error}</div>
      {/if}

      {#if loading}
        <div class="empty">Loading...</div>
      {:else}
        <form
          class="form"
          onsubmit={(e) => {
            e.preventDefault();
            void saveProvider();
          }}
        >
          <h2>LLM provider</h2>
          {#if pError}
            <div class="error">{pError}</div>
          {/if}
          {#if pNotice}
            <div class="notice">{pNotice}</div>
          {/if}
          <div class="field">
            <label>
              Provider type
              <input type="text" value={provider?.provider ?? ""} disabled />
            </label>
          </div>
          <div class="field">
            <label>
              Base URL
              <input type="url" bind:value={baseUrl} placeholder="https://api.openai.com/v1" required />
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
              Active model
              <input type="text" bind:value={model} placeholder="gpt-4o" />
            </label>
            <button type="button" class="ghost fetch" disabled={fetching} onclick={() => void fetchModels()}>
              {fetching ? "Fetching..." : "Fetch model list"}
            </button>
          </div>
          <div class="actions">
            <button class="primary" type="submit" disabled={providerSaving}>Save provider</button>
          </div>
        </form>

        <form
          class="form"
          onsubmit={(e) => {
            e.preventDefault();
            void saveSettings();
          }}
        >
          <h2>Tool permissions</h2>
          {#if sError}
            <div class="error">{sError}</div>
          {/if}
          {#if sNotice}
            <div class="notice">{sNotice}</div>
          {/if}
          <div class="field">
            <label>
              Default verdict
              <select bind:value={permissionDefault}>
                <option value="allow">Allow by default</option>
                <option value="ask">Ask before running</option>
                <option value="deny">Deny by default</option>
              </select>
            </label>
          </div>
          <div class="field">
            <label>
              Rules (name=allow, name=deny, one per line)
              <textarea bind:value={permissionRules} rows="6" placeholder="net.file_read=ask"></textarea>
            </label>
          </div>
          <div class="actions">
            <button class="primary" type="submit" disabled={settingsSaving}>Save ruleset</button>
          </div>
        </form>
      {/if}
    {/if}

    <form
      class="form"
      onsubmit={(e) => {
        e.preventDefault();
        void changePw();
      }}
    >
      <h2>Change password</h2>
      {#if pwError}
        <div class="error">{pwError}</div>
      {/if}
      {#if pwNotice}
        <div class="notice">{pwNotice}</div>
      {/if}
      <div class="field">
        <label>
          Current password
          <input type="password" bind:value={curPw} autocomplete="current-password" />
        </label>
      </div>
      <div class="field">
        <label>
          New password (at least 8 characters)
          <input type="password" bind:value={newPw} autocomplete="new-password" />
        </label>
      </div>
      <div class="field">
        <label>
          Confirm new password
          <input type="password" bind:value={confirmPw} autocomplete="new-password" />
        </label>
        {#if confirmPw !== "" && confirmPw !== newPw}
          <div class="hint">Passwords do not match.</div>
        {/if}
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={revokeOtherSessions} />
        Sign out of my other devices
      </label>
      <div class="actions">
        <button class="primary" type="submit" disabled={pwSaving || !pwValid}>
          {pwSaving ? "Saving..." : "Change password"}
        </button>
      </div>
    </form>
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
  .field select,
  .field textarea {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 10px 12px;
    outline: none;
    color: var(--text);
    font-family: inherit;
    resize: vertical;
  }

  .field input:focus,
  .field select:focus,
  .field textarea:focus {
    border-color: var(--accent);
  }

  .field .fetch {
    margin-top: 6px;
  }

  .field .fetch:disabled {
    opacity: 0.6;
    cursor: default;
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

  .hint {
    color: var(--danger);
    font-size: 12px;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--text-dim);
  }
</style>
