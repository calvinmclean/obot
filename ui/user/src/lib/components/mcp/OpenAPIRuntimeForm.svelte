<script lang="ts">
	import type { MCPConfig, OpenAPIRuntimeConfig } from '$lib/services';
	import { importOpenAPI } from '$lib/services/openapi';
	import IconButton from '../primitives/IconButton.svelte';
	import { FileUp, Trash2 } from '@lucide/svelte';
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
	let fileInput = $state<HTMLInputElement>();
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
		const file = (event.currentTarget as HTMLInputElement).files?.[0];
		if (!file) return;
		invalidate();
		config.source = {};
		fileName = file.name;
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
			<div class="flex flex-col gap-2">
				<p id="openapi-source-file-label" class="text-sm font-light">Schema file</p>
				<input
					bind:this={fileInput}
					id="openapi-source-file"
					type="file"
					class="hidden"
					accept=".json,.yaml,.yml"
					aria-labelledby="openapi-source-file-label"
					aria-describedby="openapi-file-hint"
					onchange={upload}
				/>
				<div
					class="border-base-300 bg-base-200 flex flex-wrap items-center gap-3 rounded-lg border p-4"
				>
					<FileUp class="text-muted-content size-5 shrink-0" aria-hidden="true" />
					<div class="min-w-0 flex-1 basis-40">
						<p class="text-sm font-medium break-all">
							{fileName || (config.source.content ? 'Saved schema' : 'Choose an OpenAPI schema')}
						</p>
						<p id="openapi-file-hint" class="text-muted-content mt-1 text-xs">
							{#if !fileName && config.source.content}
								The existing schema is kept unless you choose a replacement.
							{:else}
								JSON or YAML · Up to 1 MiB
							{/if}
						</p>
					</div>
					<button
						type="button"
						class="btn btn-secondary btn-sm shrink-0"
						onclick={() => fileInput?.click()}
					>
						{fileName || config.source.content ? 'Replace file' : 'Choose file'}
					</button>
				</div>
			</div>
		{/if}
		<div class="border-base-300 mt-2 flex flex-col gap-2 border-t pt-4">
			<div class="flex items-center gap-2">
				<label for="openapi-base-url" class="text-sm font-medium">API request base URL</label>
				<span class="text-muted-content bg-base-200 rounded px-2 py-0.5 text-xs">Optional</span>
			</div>
			<p id="openapi-base-url-hint" class="text-muted-content text-xs">
				Where tool calls are sent, not the schema file URL. Leave blank to use the API server URL
				defined in the schema.
			</p>
			<input
				id="openapi-base-url"
				class="text-input-filled"
				type="url"
				bind:value={config.baseURL}
				placeholder="https://api.example.com/v1"
				aria-describedby="openapi-base-url-hint"
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
					<select
						id={`openapi-method-${i}`}
						class="select w-full"
						value={rule.method?.toUpperCase() ?? ''}
						onchange={(event) => {
							rule.method = event.currentTarget.value || undefined;
						}}
					>
						<option value="">Any method</option>
						{#each ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS', 'TRACE'] as method (method)}
							<option value={method}>{method}</option>
						{/each}
					</select>
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
					{#if !readonly}
						<IconButton
							class="self-end"
							variant="danger"
							aria-label={`Remove exclusion ${i + 1}`}
							title="Remove exclusion"
							onclick={() => config.exclude?.splice(i, 1)}
						>
							<Trash2 class="size-4" aria-hidden="true" />
						</IconButton>
					{/if}
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
