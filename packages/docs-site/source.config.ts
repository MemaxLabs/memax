import {
  defineDocs,
  defineConfig,
  type DocsCollection,
} from "fumadocs-mdx/config";

export const docs: DocsCollection = defineDocs({
  dir: "content/docs",
});

export default defineConfig({
  mdxOptions: {
    rehypeCodeOptions: {
      themes: {
        light: "github-light-default",
        dark: "github-dark-default",
      },
    },
  },
});
