<script lang="ts">
  import { onMount } from "svelte";
  import { listJobs, createJob, updateJob, deleteJob, type Job } from "$lib/api";

  let jobs = $state<Job[]>([]);
  let loading = $state(true);
  let error = $state("");

  let showForm = $state(false);
  let prompt = $state("");
  let schedule = $state("");
  let model = $state("");
  let saving = $state(false);

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    error = "";
    try {
      jobs = await listJobs();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to load jobs";
    } finally {
      loading = false;
    }
  }

  async function submit() {
    if (saving || !prompt.trim() || !schedule.trim()) return;
    saving = true;
    error = "";
    try {
      await createJob({
        schedule: schedule.trim(),
        prompt: prompt.trim(),
        model: model.trim() || undefined,
      });
      prompt = "";
      schedule = "";
      model = "";
      showForm = false;
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to create job";
    } finally {
      saving = false;
    }
  }

  async function toggle(job: Job) {
    try {
      await updateJob(job.id, { enabled: !job.enabled });
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to update job";
    }
  }

  async function remove(job: Job) {
    try {
      await deleteJob(job.id);
      await refresh();
    } catch (err) {
      error = err instanceof Error ? err.message : "failed to delete job";
    }
  }

  function formatWhen(iso: string | null): string {
    if (!iso) return "—";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleString();
  }

  function statusLabel(job: Job): string {
    if (!job.last_status) return "never run";
    return job.last_status;
  }
</script>

<div class="jobs">
  <div class="column">
    <div class="toolbar">
      <h1>Scheduled jobs</h1>
      <button class="primary" onclick={() => (showForm = !showForm)}>
        {showForm ? "Cancel" : "New job"}
      </button>
    </div>

    {#if showForm}
      <form
        class="form"
        onsubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <label>
          Prompt
          <textarea bind:value={prompt} rows="2" placeholder="What should oracle do?"></textarea>
        </label>
        <label>
          Schedule
          <input bind:value={schedule} placeholder="0 8 * * *  (5-field cron)" />
        </label>
        <label>
          Model (optional)
          <input bind:value={model} placeholder="gpt-4o (empty uses the global model)" />
        </label>
        <div class="form-actions">
          <button class="primary" type="submit" disabled={saving || !prompt.trim() || !schedule.trim()}>
            Save job
          </button>
        </div>
      </form>
    {/if}

    {#if error}
      <div class="error">{error}</div>
    {/if}

    {#if loading}
      <div class="empty">Loading...</div>
    {:else if jobs.length === 0}
      <div class="empty">
        <p>No scheduled jobs yet. Create one to have oracle run a prompt on a schedule.</p>
      </div>
    {:else}
      <div class="list">
        {#each jobs as job (job.id)}
          <div class="job" class:disabled={!job.enabled}>
            <div class="job-main">
              <div class="job-prompt">{job.prompt}</div>
              <div class="job-meta">
                <span class="mono">{job.schedule}</span>
                {#if job.model}
                  <span class="pin">model: {job.model}</span>
                {/if}
                <span>next: {formatWhen(job.next_run_at)}</span>
                <span>last: {formatWhen(job.last_run_at)} ({statusLabel(job)})</span>
              </div>
            </div>
            <div class="job-actions">
              <button class="toggle" onclick={() => toggle(job)}>
                {job.enabled ? "Disable" : "Enable"}
              </button>
              <button class="delete" onclick={() => remove(job)}>Delete</button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .jobs {
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

  button.primary {
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius);
    padding: 8px 16px;
    font-weight: 600;
  }

  button.primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 12px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 16px;
  }

  .form label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    color: var(--text-dim);
  }

  .form textarea,
  .form input {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 10px 12px;
    outline: none;
    resize: vertical;
  }

  .form textarea:focus,
  .form input:focus {
    border-color: var(--accent);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
  }

  .error {
    color: var(--danger);
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

  .job {
    display: flex;
    align-items: center;
    gap: 12px;
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 12px 14px;
  }

  .job.disabled {
    opacity: 0.55;
  }

  .job-main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .job-prompt {
    font-weight: 600;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .job-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    font-size: 12px;
    color: var(--text-dim);
  }

  .mono {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }

  .pin {
    color: var(--accent);
    font-size: 12px;
  }

  .job-actions {
    display: flex;
    gap: 8px;
  }

  .job-actions button {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-input);
    color: var(--text);
    padding: 6px 12px;
    font-size: 13px;
  }

  .job-actions button.delete {
    color: var(--danger);
  }

  .job-actions button:hover {
    border-color: var(--accent);
  }
</style>
