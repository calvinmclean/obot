import { CATALOG_SERVER_FIELD_IDS } from '$lib/constants';
import type { MCPCatalogEntry } from '$lib/services';
import { createMCPCatalogEntryResponse } from '../../../tests/mocks/data';
import { worker } from '../../../tests/mocks/worker';
import CatalogServerForm from './CatalogServerForm.svelte';
import { http, HttpResponse } from 'msw';
import { beforeEach, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import { page } from 'vitest/browser';

const schema = { openapi: '3.1.0', info: { title: 'Example', version: '1' }, paths: {} };
const sourceURL = 'https://example.com/openapi.json';
const suggestedHeader = {
	key: 'Authorization',
	name: 'API key',
	usage: 'header',
	required: true,
	sensitive: true,
	prefix: 'Bearer '
};

beforeEach(() => {
	worker.use(
		http.get('/api/mcp-catalogs/default/access-control-rules', () => HttpResponse.json([]))
	);
});

async function openForm(entity: 'catalog' | 'workspace' = 'catalog') {
	await render(CatalogServerForm, { id: 'test', entity, type: 'hosted' });
	await page.getByCSS('#runtime-selector').click();
	await page.getByRole('button', { name: 'OpenAPI', exact: true }).click();
}

function mockImport(collection = 'mcp-catalogs') {
	const request = vi.fn();
	worker.use(
		http.post(`/api/${collection}/test/openapi/import`, async ({ request: req }) => {
			request(await req.json());
			return HttpResponse.json({
				schema,
				baseURL: 'https://example.com/api',
				suggestedHeaders: [suggestedHeader]
			});
		})
	);
	return request;
}

it('imports before saving and keeps the snapshot, header prefix, and exclusion settings', async () => {
	const imported = mockImport();
	const saved = vi.fn();
	worker.use(
		http.post('/api/mcp-catalogs/test/entries', async ({ request }) => {
			saved(await request.json());
			return HttpResponse.json(createMCPCatalogEntryResponse);
		})
	);
	await openForm();
	await expect.element(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	await page.getByLabelText('Schema URL', { exact: true }).fill(sourceURL);
	await page.getByRole('button', { name: 'Import schema', exact: true }).click();
	await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
	expect(imported).toHaveBeenCalledWith({ source: { url: sourceURL } });
	await expect.element(page.getByLabelText('Value Prefix')).toHaveValue('Bearer ');
	await page.getByLabelText('Enable Tool Search').click();
	await page.getByRole('button', { name: 'Add exclusion' }).click();
	await page.getByLabelText('Method', { exact: true }).selectOptions('POST');
	await page.getByLabelText('Path pattern (regex)').fill('^/users$');
	await page.getByLabelText('Tag', { exact: true }).fill('internal');
	await page.getByLabelText('API request base URL').fill('https://override.example.com');
	await page.getByCSS(`#${CATALOG_SERVER_FIELD_IDS.name}`).fill('Example API');
	await page.getByCSS(`#${CATALOG_SERVER_FIELD_IDS.shortDescription}`).fill('An example API');
	await page.getByRole('button', { name: 'Save', exact: true }).click();
	await vi.waitFor(() => expect(saved).toHaveBeenCalled());
	expect(saved.mock.calls[0][0]).toMatchObject({
		runtime: 'openapi',
		openAPIConfig: {
			source: { url: sourceURL },
			schema,
			baseURL: 'https://override.example.com',
			toolSearch: true,
			exclude: [{ method: 'POST', pathPattern: '^/users$', tag: 'internal' }]
		},
		config: [suggestedHeader]
	});
});

it('uses the workspace import endpoint and invalidates the snapshot after changing its source', async () => {
	const imported = mockImport('workspaces');
	await openForm('workspace');
	await page.getByLabelText('Schema URL', { exact: true }).fill(sourceURL);
	await page.getByRole('button', { name: 'Import schema', exact: true }).click();
	await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
	expect(imported).toHaveBeenCalledOnce();
	await page.getByLabelText('Schema URL', { exact: true }).fill('https://example.com/new.json');
	await expect.element(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	await expect.element(page.getByRole('status')).not.toBeInTheDocument();
});

it('shows import errors and allows retry without creating an entry', async () => {
	worker.use(
		http.post(
			'/api/mcp-catalogs/test/openapi/import',
			() => new HttpResponse('schema must be a JSON or YAML document', { status: 400 })
		)
	);
	await openForm();
	await page.getByLabelText('Schema URL', { exact: true }).fill(sourceURL);
	await page.getByRole('button', { name: 'Import schema', exact: true }).click();
	await expect
		.element(page.getByRole('alert'))
		.toHaveTextContent('schema must be a JSON or YAML document');
	await expect.element(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	mockImport();
	await page.getByRole('button', { name: 'Import schema', exact: true }).click();
	await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
});

it('preserves an existing snapshot without importing and removes exclusions when search is disabled', async () => {
	const imported = mockImport();
	const entry = {
		...createMCPCatalogEntryResponse,
		manifest: {
			...createMCPCatalogEntryResponse.manifest,
			runtime: 'openapi',
			openAPIConfig: {
				source: { url: sourceURL },
				schema,
				toolSearch: true,
				exclude: [{ method: 'DELETE' }]
			}
		}
	} as MCPCatalogEntry;
	await render(CatalogServerForm, { id: 'test', entity: 'catalog', entry });
	await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
	await expect.element(page.getByLabelText('Method', { exact: true })).toHaveValue('DELETE');
	await page.getByLabelText('Enable Tool Search').click();
	await expect.element(page.getByRole('button', { name: 'Add exclusion' })).not.toBeInTheDocument();
	await page.getByLabelText('Enable Tool Search').click();
	await expect.element(page.getByLabelText('Method', { exact: true })).not.toBeInTheDocument();
	expect(imported).not.toHaveBeenCalled();
	expect(entry.manifest.openAPIConfig?.exclude).toEqual([{ method: 'DELETE' }]);
});

it.each([
	{ name: 'api.json', content: JSON.stringify(schema) },
	{
		name: 'api.yaml',
		content: 'openapi: 3.1.0\ninfo:\n  title: Example\n  version: "1"\npaths: {}'
	}
])(
	'uploads $name as source content and supports an API without credentials',
	async ({ name, content }) => {
		const imported = vi.fn();
		worker.use(
			http.post('/api/mcp-catalogs/test/openapi/import', async ({ request }) => {
				imported(await request.json());
				return HttpResponse.json({ schema, baseURL: 'https://example.com', suggestedHeaders: [] });
			})
		);
		await openForm();
		await page.getByLabelText('Schema source').selectOptions('file');
		await page.getByLabelText('Schema file', { exact: true }).upload(new File([content], name));
		await expect.element(page.getByText(name, { exact: true })).toBeVisible();
		await expect
			.element(page.getByRole('button', { name: 'Replace file', exact: true }))
			.toBeVisible();
		await page.getByRole('button', { name: 'Import schema', exact: true }).click();
		await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
		expect(imported).toHaveBeenCalledWith({ source: { content } });
		await expect.element(page.getByLabelText('Value Prefix')).not.toBeInTheDocument();
	}
);

it('shows an existing uploaded schema without asking users to select it again', async () => {
	await render(CatalogServerForm, {
		id: 'test',
		entity: 'catalog',
		entry: {
			...createMCPCatalogEntryResponse,
			manifest: {
				...createMCPCatalogEntryResponse.manifest,
				runtime: 'openapi',
				openAPIConfig: { source: { content: JSON.stringify(schema) }, schema }
			}
		} as MCPCatalogEntry
	});
	await expect.element(page.getByText('Saved schema', { exact: true })).toBeVisible();
	await expect
		.element(page.getByText('The existing schema is kept unless you choose a replacement.'))
		.toBeVisible();
	await expect
		.element(page.getByRole('button', { name: 'Replace file', exact: true }))
		.toBeEnabled();
	await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
});

it('opens the file picker only from the choose button, not the heading', async () => {
	await openForm();
	await page.getByLabelText('Schema source').selectOptions('file');
	const pickerClick = vi.fn();
	page
		.getByLabelText('Schema file', { exact: true })
		.element()
		.addEventListener('click', (event) => {
			// Observe activation without opening an operating-system dialog in the test.
			event.preventDefault();
			pickerClick();
		});
	await page.getByText('Schema file', { exact: true }).click();
	expect(pickerClick).not.toHaveBeenCalled();
	await page.getByRole('button', { name: 'Choose file', exact: true }).click();
	expect(pickerClick).toHaveBeenCalledOnce();
});

it('allows any method and removes exclusions using the icon button', async () => {
	await openForm();
	await page.getByLabelText('Enable Tool Search').click();
	await page.getByRole('button', { name: 'Add exclusion' }).click();
	const method = page.getByRole('combobox', { name: 'Method', exact: true });
	await expect.element(method).toHaveValue('');
	await method.selectOptions('DELETE');
	await expect.element(method).toHaveValue('DELETE');
	await method.selectOptions('');
	await expect.element(method).toHaveValue('');
	await page.getByRole('button', { name: 'Remove exclusion 1', exact: true }).click();
	await expect.element(method).not.toBeInTheDocument();
});

it('rejects oversized files before import', async () => {
	const imported = mockImport();
	await openForm();
	await page.getByLabelText('Schema source').selectOptions('file');
	await page
		.getByLabelText('Schema file', { exact: true })
		.upload(new File([new Uint8Array(1024 * 1024 + 1)], 'large.json'));
	await expect.element(page.getByRole('alert')).toHaveTextContent('no larger than 1 MiB');
	await expect
		.element(page.getByRole('button', { name: 'Import schema', exact: true }))
		.toBeDisabled();
	expect(imported).not.toHaveBeenCalled();
});

it('shows stored configuration read-only without offering import or exclusion edits', async () => {
	await render(CatalogServerForm, {
		id: 'test',
		entity: 'catalog',
		readonly: true,
		entry: {
			...createMCPCatalogEntryResponse,
			manifest: {
				...createMCPCatalogEntryResponse.manifest,
				runtime: 'openapi',
				openAPIConfig: {
					source: { url: sourceURL },
					schema,
					toolSearch: true,
					exclude: [{ tag: 'internal' }]
				}
			}
		} as MCPCatalogEntry
	});
	await expect.element(page.getByLabelText('Schema URL', { exact: true })).toBeDisabled();
	await expect.element(page.getByLabelText('Enable Tool Search')).toBeDisabled();
	await expect.element(page.getByLabelText('Tag', { exact: true })).toBeDisabled();
	await expect
		.element(page.getByRole('button', { name: 'Import schema', exact: true }))
		.not.toBeInTheDocument();
	await expect.element(page.getByRole('button', { name: 'Add exclusion' })).not.toBeInTheDocument();
});

it('keeps edited header flags and prefixes and avoids duplicates on another import', async () => {
	const imported = mockImport();
	await openForm();
	await page.getByLabelText('Schema URL', { exact: true }).fill(sourceURL);
	await page.getByRole('button', { name: 'Import schema', exact: true }).click();
	await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
	await page.getByLabelText('Value Prefix').fill('Token ');
	const required = page.getByRole('switch', { name: 'Required', exact: true });
	const sensitive = page.getByRole('switch', { name: 'Sensitive', exact: true });
	await expect.element(required).toBeChecked();
	await expect.element(sensitive).toBeChecked();
	await required.click();
	await sensitive.click();
	await page.getByRole('button', { name: 'Import schema', exact: true }).click();
	await vi.waitFor(() => expect(imported).toHaveBeenCalledTimes(2));
	await expect.element(page.getByRole('status')).toHaveTextContent('Schema imported');
	await expect.element(page.getByLabelText('Value Prefix')).toHaveValue('Token ');
	await expect.element(required).not.toBeChecked();
	await expect.element(sensitive).not.toBeChecked();
});

it('locks source edits during import and ignores the result after changing runtimes', async () => {
	let finishImport!: () => void;
	const responseReady = new Promise<void>((resolve) => {
		finishImport = resolve;
	});
	worker.use(
		http.post('/api/mcp-catalogs/test/openapi/import', async () => {
			await responseReady;
			return HttpResponse.json({
				schema,
				baseURL: 'https://example.com',
				suggestedHeaders: [suggestedHeader]
			});
		})
	);
	await openForm();
	await page.getByLabelText('Schema URL', { exact: true }).fill(sourceURL);
	await page.getByRole('button', { name: 'Import schema', exact: true }).click();
	await expect.element(page.getByLabelText('Schema URL', { exact: true })).toBeDisabled();
	await expect.element(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
	await page.getByCSS('#runtime-selector').click();
	await page.getByRole('button', { name: 'NPX', exact: true }).click();
	finishImport();
	await expect.element(page.getByCSS('#npx-package')).toBeVisible();
	await expect.element(page.getByLabelText('Value Prefix')).not.toBeInTheDocument();
});
