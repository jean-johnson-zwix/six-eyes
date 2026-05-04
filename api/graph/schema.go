package graph

// Schema is the GraphQL schema definition.
const Schema = `
	type Paper {
		# Arxiv identifier (e.g. "2401.12345")
		arxivId:          String!
		title:            String!
		authors:          [String!]!
		categories:       [String!]!
		abstract:         String!
		submittedAt:      String!
		hasCode:          Boolean!

		# Model-computed fields
		hypeScore:         Float!
		hypeTier:         String!

		# Enrichment fields (null if not yet fetched)
		maxHIndex:        Int
		totalPriorPapers: Int
		hfUpvotes:        Int
	}

	type Query {
		# papers returns recently ingested papers ranked by hype score.
		# days:  how many days back to look (default 30)
		# limit: max results (default 50, max 200)
		# tier:  filter by tier — "hype", "likely", or "low" (default: all)
		papers(days: Int, limit: Int, tier: String): [Paper!]!

		# paper returns a single paper by Arxiv ID, or null if not found.
		paper(arxivId: String!): Paper
	}
`
