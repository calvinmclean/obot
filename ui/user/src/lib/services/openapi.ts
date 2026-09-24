import type { MCPConfig, OpenAPIRuntimeConfig } from '.';
import { doPost } from './http';

export interface OpenAPIImportResult {
	schema: Record<string, unknown>;
	baseURL: string;
	suggestedHeaders: MCPConfig[];
}

export async function importOpenAPI(
	entity: 'catalog' | 'workspace',
	id: string,
	config: OpenAPIRuntimeConfig
): Promise<OpenAPIImportResult> {
	const collection = entity === 'workspace' ? 'workspaces' : 'mcp-catalogs';
	return (await doPost(
		`/${collection}/${encodeURIComponent(id)}/openapi/import`,
		{ source: config.source, baseURL: config.baseURL },
		{ dontLogErrors: true }
	)) as OpenAPIImportResult;
}
