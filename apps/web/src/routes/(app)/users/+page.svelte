<script lang="ts">
  import { onMount } from "svelte";
  import { listUsers, createUser, deleteUser, resetUserPassword, type AdminUser } from "$lib/api";
  import type { PageData } from "./$types";

  let { data } = $props<{ data: PageData }>();

  let users = $state<AdminUser[]>([]);
  let loading = $state(true);
  let error = $state("");
  let notice = $state("");

  let email = $state("");
  let password = $state("");
  let saving = $state(false);

  let resettingId = $state<number | null>(null);
  let resetPassword = $state("");
  let resetting = $state(false);

  let isAdmin = $derived(data.user?.role === "admin");
  let myEmail = $derived(data.user?.email ?? "");

  onMount(() => {
    if (isAdmin) void refresh();
  });

  async function refresh() {
    error = "";
    try {
      users = await listUsers();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to load users";
    } finally {
      loading = false;
    }
  }

  async function submit() {
    if (saving || !email.trim() || !password) return;
    saving = true;
    error = "";
    notice = "";
    try {
      await createUser(email.trim(), password);
      email = "";
      password = "";
      notice = "User created.";
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to create user";
    } finally {
      saving = false;
    }
  }

  async function remove(u: AdminUser) {
    error = "";
    notice = "";
    try {
      await deleteUser(u.id);
      notice = `Deleted ${u.email}.`;
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to delete user";
    }
  }

  async function resetPw(u: AdminUser) {
    if (resetting || !resetPassword) return;
    resetting = true;
    error = "";
    notice = "";
    try {
      await resetUserPassword(u.id, resetPassword);
      resetPassword = "";
      resettingId = null;
      notice = `Reset ${u.email}'s password. They are signed out of other devices.`;
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to reset password";
    } finally {
      resetting = false;
    }
  }

  function formatWhen(iso: string): string {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleString();
  }
</script>

<div class="users">
  <div class="column">
    <div class="toolbar">
      <h1>Users</h1>
    </div>

    {#if !isAdmin}
      <div class="empty"><p>Only administrators can manage users.</p></div>
    {:else}
      <form
        class="form"
        onsubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <div class="form-row">
          <label>
            Email
            <input type="email" bind:value={email} placeholder="person@example.com" />
          </label>
          <label>
            Password
            <input type="password" bind:value={password} placeholder="At least 8 characters" />
          </label>
          <button class="primary" type="submit" disabled={saving || !email.trim() || !password}>Add user</button>
        </div>
      </form>

      {#if error}
        <div class="error">{error}</div>
      {/if}
      {#if notice}
        <div class="notice">{notice}</div>
      {/if}

      {#if loading}
        <div class="empty">Loading...</div>
      {:else}
        <div class="list">
          {#each users as u (u.id)}
            <div class="user">
              <div class="user-main">
                <span class="user-email">{u.email}</span>
                <span class="badge" class:admin={u.role === "admin"}>{u.role}</span>
                <span class="user-meta">created {formatWhen(u.created_at)}</span>
              </div>
              {#if u.email === myEmail}
                <span class="you">you</span>
              {:else if resettingId === u.id}
                <form
                  class="reset-form"
                  onsubmit={(e) => {
                    e.preventDefault();
                    void resetPw(u);
                  }}
                >
                  <input
                    type="password"
                    bind:value={resetPassword}
                    placeholder="New password (at least 8 characters)"
                    autocomplete="off"
                  />
                  <button
                    class="ghost"
                    type="button"
                    onclick={() => {
                      resettingId = null;
                      resetPassword = "";
                    }}>Cancel</button
                  >
                  <button class="primary small" type="submit" disabled={resetting || resetPassword.length < 8}>
                    {resetting ? "Saving..." : "Save"}
                  </button>
                </form>
              {:else}
                <div class="row-actions">
                  <button
                    class="ghost"
                    onclick={() => {
                      resettingId = u.id;
                      resetPassword = "";
                    }}>Reset password</button
                  >
                  <button class="delete" onclick={() => void remove(u)}>Delete</button>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .users {
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

  .form {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 16px;
  }

  .form-row {
    display: flex;
    gap: 12px;
    align-items: flex-end;
    flex-wrap: wrap;
  }

  .form-row label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    color: var(--text-dim);
    flex: 1;
    min-width: 180px;
  }

  .form-row input {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 10px 12px;
    outline: none;
  }

  .form-row input:focus {
    border-color: var(--accent);
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

  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .user {
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 12px 14px;
  }

  .user-main {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  .user-email {
    font-weight: 600;
  }

  .badge {
    font-size: 12px;
    padding: 2px 8px;
    border-radius: 999px;
    border: 1px solid var(--border);
    color: var(--text-dim);
  }

  .badge.admin {
    color: var(--accent);
    border-color: var(--accent);
  }

  .user-meta {
    font-size: 12px;
    color: var(--text-dim);
  }

  .you {
    font-size: 13px;
    color: var(--text-dim);
  }

  .delete {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-input);
    color: var(--danger);
    padding: 6px 12px;
    font-size: 13px;
  }

  .delete:hover {
    border-color: var(--danger);
  }

  .row-actions {
    display: flex;
    gap: 8px;
  }

  .ghost {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-input);
    color: var(--text);
    padding: 6px 12px;
    font-size: 13px;
  }

  .ghost:hover {
    border-color: var(--accent);
  }

  .primary.small {
    font-size: 13px;
    padding: 7px 12px;
  }

  .reset-form {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .reset-form input {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 7px 10px;
    font-size: 13px;
    outline: none;
    width: 220px;
  }

  .reset-form input:focus {
    border-color: var(--accent);
  }
</style>
