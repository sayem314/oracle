<script lang="ts">
  import { goto } from "$app/navigation";
  import { page } from "$app/state";
  import { signOut } from "$lib/api";
  import type { LayoutData } from "./$types";

  let { data, children } = $props<{ data: LayoutData; children: import("svelte").Snippet }>();

  let isAdmin = $derived(data.user.role === "admin");
  let path = $derived(page.url.pathname);

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
    <div class="left">
      <span class="brand">oracle</span>
      <nav>
        <a href="/" class:active={path === "/"}>Chat</a>
        <a href="/jobs" class:active={path.startsWith("/jobs")}>Jobs</a>
        {#if isAdmin}
          <a href="/users" class:active={path.startsWith("/users")}>Users</a>
          <a href="/settings" class:active={path.startsWith("/settings")}>Settings</a>
        {/if}
      </nav>
    </div>
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

  .left {
    display: flex;
    align-items: center;
    gap: 20px;
  }

  .brand {
    font-weight: 700;
    letter-spacing: 0.5px;
  }

  nav {
    display: flex;
    gap: 4px;
  }

  nav a {
    color: var(--text-dim);
    padding: 6px 10px;
    border-radius: 8px;
    font-size: 14px;
    text-decoration: none;
  }

  nav a:hover {
    color: var(--text);
    text-decoration: none;
  }

  nav a.active {
    color: var(--text);
    background: var(--bg-input);
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
