export interface Paper {
  arxivId: string;
  title: string;
  authors: string[];
  categories: string[];
  abstract: string;
  submittedAt: string;
  hasCode: boolean;
  hypeScore: number;
  hypeTier: 'hype' | 'likely' | 'low';
  maxHIndex: number | null;
  totalPriorPapers: number | null;
  hfUpvotes: number | null;
}

const PAPER_FIELDS = `
  arxivId title authors categories abstract submittedAt
  hasCode hypeScore hypeTier maxHIndex totalPriorPapers hfUpvotes
`;

const PAPERS_QUERY = `
  query Papers($days: Int, $limit: Int, $tier: String) {
    papers(days: $days, limit: $limit, tier: $tier) { ${PAPER_FIELDS} }
  }
`;

const PAPER_QUERY = `
  query Paper($arxivId: String!) {
    paper(arxivId: $arxivId) { ${PAPER_FIELDS} }
  }
`;

async function gql<T>(
  query: string,
  variables: Record<string, unknown>,
  fetchOptions: RequestInit,
): Promise<T> {
  const url = process.env.API_URL;
  const key = process.env.API_KEY;
  if (!url || !key) throw new Error('API_URL and API_KEY env vars are required');

  const res = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${key}`,
    },
    body: JSON.stringify({ query, variables }),
    ...fetchOptions,
  });

  if (!res.ok) throw new Error(`GraphQL request failed: ${res.status} ${res.statusText}`);

  const json = await res.json() as { data?: T; errors?: { message: string }[] };
  if (json.errors?.length) throw new Error(json.errors[0].message);
  if (!json.data) throw new Error('No data in GraphQL response');
  return json.data;
}

export async function fetchPapers(
  days = 30,
  limit = 50,
  tier?: string,
): Promise<Paper[]> {
  const data = await gql<{ papers: Paper[] }>(
    PAPERS_QUERY,
    { days, limit, tier: tier || null },
    { next: { revalidate: 3600 } }, // re-fetch at most once per hour
  );
  return data.papers;
}

export async function fetchPaper(arxivId: string): Promise<Paper | null> {
  const data = await gql<{ paper: Paper | null }>(
    PAPER_QUERY,
    { arxivId },
    { cache: 'no-store' },
  );
  return data.paper;
}
