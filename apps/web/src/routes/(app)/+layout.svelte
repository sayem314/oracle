<script lang="ts">
  import { goto } from "$app/navigation";
  import { signOut } from "$lib/api";
  import type { LayoutData } from "./$types";

  let { data, children } = $props<{ data: LayoutData; children: import("svelte").Snippet }>();

  async function onSignOut() {
    try {
      await signOut();
    } finally {
      await goto("/login");
    }
  }
</script>

<div class="shell">
  <header>
    <span class="brand">oracle</span>
    <div class="right">
      <span class="email">{data.user.email}</span>
      <button type="button" onclick={onSignOut}>Sign out</button>
    </div>
  </header>

  <main>
    {@render children()}
  </main>
</div>

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 20px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-raised);
  }

  .brand {
    font-weight: 700;
    letter-spacing: 0.5px;
  }

  .right {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .email {
    color: var(--text-dim);
    font-size: 13px;
  }

  header button {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--text);
    padding: 6px 12px;
    font-size: 13px;
  }

  header button:hover {
    border-color: var(--accent);
  }

  main {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
</style>
