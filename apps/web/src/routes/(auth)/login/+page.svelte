<script lang="ts">
  import { goto } from "$app/navigation";
  import { signIn, signUp } from "$lib/api";

  type Mode = "signin" | "signup";

  let mode = $state<Mode>("signin");
  let email = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);

  function switchMode(next: Mode) {
    mode = next;
    error = "";
  }

  async function onSubmit(e: Event) {
    e.preventDefault();
    error = "";
    busy = true;
    try {
      if (mode === "signin") {
        await signIn(email, password);
      } else {
        await signUp(email, password);
      }
      await goto("/");
    } catch (err) {
      error = err instanceof Error ? err.message : "something went wrong";
    } finally {
      busy = false;
    }
  }
</script>

<div class="wrap">
  <div class="card">
    <h1>oracle</h1>
    <p class="sub">Your autonomous assistant</p>

    <div class="tabs" role="tablist">
      <button type="button" class:active={mode === "signin"} onclick={() => switchMode("signin")} role="tab">
        Sign in
      </button>
      <button type="button" class:active={mode === "signup"} onclick={() => switchMode("signup")} role="tab">
        Create account
      </button>
    </div>

    <form onsubmit={onSubmit}>
      <label>
        <span>Email</span>
        <input type="email" name="email" autocomplete="email" bind:value={email} required />
      </label>

      <label>
        <span>Password</span>
        <input
          type="password"
          name="password"
          autocomplete={mode === "signin" ? "current-password" : "new-password"}
          bind:value={password}
          required
        />
      </label>

      {#if error}
        <p class="error">{error}</p>
      {/if}

      <button type="submit" class="primary" disabled={busy}>
        {busy ? "Working..." : mode === "signin" ? "Sign in" : "Create account"}
      </button>
    </form>

    {#if mode === "signin"}
      <p class="hint">
        First time here? <button type="button" class="link" onclick={() => switchMode("signup")}
          >Create an account</button
        >.
      </p>
    {/if}
  </div>
</div>

<style>
  .wrap {
    min-height: 100vh;
    display: grid;
    place-items: center;
    padding: 24px;
  }

  .card {
    width: 100%;
    max-width: 380px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 28px;
  }

  h1 {
    margin: 0;
    font-size: 24px;
    letter-spacing: 0.5px;
  }

  .sub {
    margin: 4px 0 20px;
    color: var(--text-dim);
    font-size: 13px;
  }

  .tabs {
    display: flex;
    gap: 6px;
    margin-bottom: 18px;
  }

  .tabs button {
    flex: 1;
    padding: 8px 10px;
    border-radius: 8px;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--text-dim);
  }

  .tabs button.active {
    background: var(--bg-input);
    color: var(--text);
    border-color: var(--accent);
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    color: var(--text-dim);
  }

  input {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 12px;
    outline: none;
  }

  input:focus {
    border-color: var(--accent);
  }

  button.primary {
    margin-top: 4px;
    padding: 11px 12px;
    border: none;
    border-radius: 8px;
    background: var(--accent);
    color: #fff;
    font-weight: 600;
  }

  button.primary:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 13px;
  }

  .hint {
    margin: 16px 0 0;
    color: var(--text-dim);
    font-size: 13px;
  }

  button.link {
    background: none;
    border: none;
    padding: 0;
    color: var(--accent);
    font-size: 13px;
  }

  button.link:hover {
    text-decoration: underline;
  }
</style>
