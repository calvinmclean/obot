<script lang="ts">
	import type { MCPConfig, OpenAPIRuntimeConfig } from '$lib/services';
	import { importOpenAPI } from '$lib/services/openapi';
	import { onDestroy, untrack } from 'svelte';

	interface Props {
		config: OpenAPIRuntimeConfig;
		headers?: MCPConfig[];
		entity: 'catalog' | 'workspace';
		id: string;
		readonly?: boolean;
	}
	let {
		config = $bindable(),
		headers = $bindable(),
		entity,
		id,
		readonly = false
	}: Props = $props();
	let sourceType = $state(untrack(() => (config.source.content !== undefined ? 'file' : 'url')));
	let busy = $state(false);
	let error = $state('');
	let resolvedBaseURL = $state('');
	let fileName = $state('');
	let disposed = false;
	onDestroy(() => {
		disposed = true;
	});

	function invalidate() {
		config.schema = undefined;
		resolvedBaseURL = '';
		error = '';
	}

	async function upload(event: Event) {
		invalidate();
		config.source = {};
		const file = (event.currentTarget as HTMLInputElement).files?.[0];
		fileName = file?.name ?? '';
		if (!file) return;
		if (file.size > 1024 * 1024) {
			error = 'Schema files must be no larger than 1 MiB.';
			return;
		}
		busy = true;
		try {
			const content = await file.text();
			if (!disposed) config.source = { content };
		} catch {
			error = 'Unable to read the schema file.';
		} finally {
			busy = false;
		}
	}

	async function importSchema() {
		busy = true;
		invalidate();
		try {
			const result = await importOpenAPI(entity, id, config);
			if (disposed) return;
			config.schema = result.schema;
			resolvedBaseURL = result.baseURL;
			headers ??= [];
			for (const suggested of result.suggestedHeaders) {
				if (!headers.some((header) => header.key.toLowerCase() === suggested.key.toLowerCase())) {
					headers.push(suggested);
				}
			}
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			busy = false;
		}
	}
</script>

<section class="paper flex flex-col gap-4 p-4" aria-label="OpenAPI configuration">
	<h4 class="text-sm font-semibold">OpenAPI</h4>
	<p class="text-muted-content text-xs">
		Import a schema to create tools for an HTTP API. API keys are supported; OAuth is not supported.
	</p>
	<fieldset disabled={readonly || busy} class="flex flex-col gap-4">
		<div class="flex flex-col gap-1">
			<label for="openapi-source-type">Schema source</label>
			<select
				id="openapi-source-type"
				class="select w-full"
				bind:value={sourceType}
				onchange={() => {
					config.source = {};
					fileName = '';
					invalidate();
				}}
			>
				<option value="url">URL</option>
				<option value="file">Upload file</option>
			</select>
		</div>
		{#if sourceType === 'url'}
			<div class="flex flex-col gap-1">
				<label for="openapi-source-url">Schema URL</label>
				<input
					id="openapi-source-url"
					class="text-input-filled"
					type="url"
					placeholder="https://api.example.com/openapi.json"
					bind:value={config.source.url}
					oninput={invalidate}
				/>
			</div>
		{:else if !readonly}
			<div class="flex flex-col gap-1">
				<label for="openapi-source-file">Schema file (JSON or YAML, up to 1 MiB)</label>
				<input id="openapi-source-file" type="file" accept=".json,.yaml,.yml" onchange={upload} />
				{#if fileName}<p class="text-xs">{fileName}</p>{/if}
			</div>
		{/if}
		<div class="flex flex-col gap-1">
			<label for="openapi-base-url">API base URL override (optional)</label>
			<input
				id="openapi-base-url"
				class="text-input-filled"
				type="url"
				bind:value={config.baseURL}
				placeholder="Use the server URL from the schema"
			/>
		</div>
		{#if !readonly}
			<button
				type="button"
				class="btn btn-secondary self-start"
				onclick={importSchema}
				disabled={!config.source.url?.trim() && !config.source.content}
			>
				{busy ? 'Importing…' : 'Import schema'}
			</button>
		{/if}
	</fieldset>
	{#if error}<p role="alert" class="text-sm text-error">{error}</p>{/if}
	{#if config.schema}
		<p role="status" class="text-sm">
			Schema imported. The snapshot will be saved with this entry.
		</p>
		{#if resolvedBaseURL}<p class="text-muted-content text-xs">
				Imported API base URL: {resolvedBaseURL}
			</p>{/if}
	{:else}
		<p class="text-muted-content text-xs">Import the schema before saving this entry.</p>
	{/if}
	<fieldset disabled={readonly} class="flex flex-col gap-4">
		<label class="flex items-center gap-2">
			<input
				class="checkbox checkbox-sm"
				type="checkbox"
				bind:checked={config.toolSearch}
				onchange={() => {
					if (!config.toolSearch) config.exclude = [];
				}}
			/>
			Enable Tool Search
		</label>
		<p class="text-muted-content text-xs">
			Tool Search lets agents find tools as needed. Without it, use a vMCP to filter tools.
		</p>
		{#if config.toolSearch}
			<h5 class="text-sm font-semibold">Exclusions</h5>
			<p class="text-muted-content text-xs">
				All fields in a rule must match. Any matching rule excludes an operation. Disabling Tool
				Search removes these rules.
			</p>
			{#each config.exclude ?? [] as rule, i (i)}
				<div class="flex flex-col gap-2 rounded border p-3">
					<label for={`openapi-method-${i}`}>Method</label>
					<input
						id={`openapi-method-${i}`}
						class="text-input-filled"
						placeholder="POST"
						bind:value={rule.method}
					/>
					<label for={`openapi-path-${i}`}>Path pattern (regex)</label>
					<input
						id={`openapi-path-${i}`}
						class="text-input-filled"
						placeholder="^/users$"
						bind:value={rule.pathPattern}
					/>
					<label for={`openapi-tag-${i}`}>Tag</label>
					<input
						id={`openapi-tag-${i}`}
						class="text-input-filled"
						placeholder="internal"
						bind:value={rule.tag}
					/>
					{#if !readonly}<button
							type="button"
							class="btn btn-ghost self-start"
							onclick={() => config.exclude?.splice(i, 1)}>Remove exclusion {i + 1}</button
						>{/if}
				</div>
			{/each}
			{#if !readonly}<button
					type="button"
					class="btn btn-secondary self-start"
					onclick={() => {
						config.exclude ??= [];
						config.exclude.push({});
					}}>Add exclusion</button
				>{/if}
		{/if}
	</fieldset>
</section>
