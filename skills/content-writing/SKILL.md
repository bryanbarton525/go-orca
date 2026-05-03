# Content Writing Skill

Use this skill when producing blog posts, articles, changelog entries, or any long-form technical writing.

## Voice and Tone

- Write in the second person ("you") for tutorials; first person ("we") for retrospectives and decision posts.
- Prefer active voice. Eliminate "it should be noted that" and similar throat-clearing phrases.
- Be specific. "Reduced p99 latency from 240 ms to 18 ms" beats "significantly faster."

## Post Structure

Every blog post must have:

1. **Hook** — One sentence or short paragraph that makes the reader want to continue.
2. **Context** — Why does this problem exist? Who cares?
3. **Body** — The meat: code, diagrams, numbered steps, benchmarks.
4. **Conclusion** — What did we learn? What should the reader do next?
5. **CTA** — Call to action: link to docs, repo, Discord, or next post.

See [blog-post-template.md](assets/blog-post-template.md) for a ready-to-fill template.

## Word Count Conventions

For technical articles that include embedded code examples, word count is measured against **prose body text only**. The following are excluded from the word count:

- All lines inside fenced code blocks (` ``` ` ... ` ``` `)
- Import statements and package declarations within code blocks
- Inline code comments (`// ...`) inside code blocks
- YAML frontmatter

Rationale: code blocks are reference material, not narrative prose. Counting them inflates the apparent length and distorts the 400–600 word constraint typical of focused technical guides.

When specifying or verifying a word-count target:
- State it as "N words of body prose, excluding code blocks" to avoid ambiguity.
- QA validates against this same convention — QA should count prose only.
- Matriarch should resolve word-count scope in the review thread before Architect produces the task graph so every downstream persona uses the same baseline.

## Code Blocks

- Always specify the language for syntax highlighting (` ```go `, ` ```yaml `, etc.).
- Show idiomatic code, not pseudo-code, unless length is the concern.
- Prefer short, self-contained examples over long file dumps; link to the repo for full context.

## SEO and Metadata

- Title: 50–60 characters, keyword-first.
- Description (frontmatter): 120–160 characters, includes the primary keyword.
- One `h1` (the title); `h2` for major sections; `h3` for subsections.

## References

- [blog-structure.md](references/blog-structure.md) — detailed style guide including word count targets and common mistakes
