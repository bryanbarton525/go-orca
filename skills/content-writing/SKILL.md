---
name: content-writing
description: Techniques for writing clear, compelling technical blog posts and documentation.
---

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
