# site/ — the project's website

One static page: `index.html` and `style.css`, with no script and no build step. The workflow
[`.github/workflows/pages.yml`](../.github/workflows/pages.yml) publishes this folder to GitHub Pages on each push to
`master` that changes it.

- To see it, open `index.html` in a browser. Every path is relative.
- The look is the application's: the tokens in `style.css` are a subset of the token block in
  [`extensions/web/view/html/base.css`](../extensions/web/view/html/base.css), with the same values. When a token changes there,
  change it here too. Three tokens are the site's own, and the application does not have them: the alias
  `--text-accent` (the accent colour of the words on a material), `--wide` (the largest width of the page's column)
  and `--gutter` (the space at the two sides of the column).
- `fonts/` and `icon.svg` are copies of the own files of the browser, `aminte-web`, in
  [`extensions/web/view/html/`](../extensions/web/view/html/). The fonts are under the SIL Open Font License; the `OFL-*.txt` files
  go with them.
- The day page in the hero is drawn in HTML. Its content is an example. Never put real data on this page.
