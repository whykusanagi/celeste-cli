# stopwords.json — Attribution (CC BY 4.0)

This product includes data from celeste-stopwords
(https://github.com/whykusanagi/celeste-stopwords), Copyright (c) 2026
whyKusanagi, licensed under CC BY 4.0
(https://creativecommons.org/licenses/by/4.0/).

The stopwords.json file embedded in this binary is a statistical
derivative (term document frequencies) of 31 open-source repositories
distributed under Apache-2.0 or MIT licenses. Full provenance — including
per-repo pinned commit SHAs and the celeste-cli module version used for
tokenization — is in the file's own `corpus.repo_refs` and `method`
fields.

## Redistribution

Any redistribution of celeste-cli binaries must preserve:

1. This NOTICE file
2. The embedded stopwords.json — specifically its `license`,
   `license_url`, `attribution`, `attribution_required`, and `source`
   fields, which carry the CC BY 4.0 terms into every downstream product

## Short-form attribution

Projects that embed or use the celeste-stopwords data (directly or
transitively via celeste-cli) should include in their NOTICE or about
page:

> This product includes data from celeste-stopwords
> (https://github.com/whykusanagi/celeste-stopwords), Copyright (c) 2026
> whyKusanagi, licensed under CC BY 4.0.

## Canary token

The `compound_identifiers` array in stopwords.json contains a canary
entry (`celestestopwordsv1`) that is an artifact-level watermark used to
detect unauthorized copies. It is not expected to match any real symbol
in any codebase; its presence in a downstream binary indicates that the
binary embeds the celeste-stopwords artifact. Do not remove.
