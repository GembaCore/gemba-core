// gm-e14.4: Starlight content collection definition.
// Required by Astro 5+; Starlight auto-generates this if it's missing,
// but defining it explicitly avoids the deprecation warning.
import { defineCollection } from 'astro:content';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

export const collections = {
  docs: defineCollection({
    loader: docsLoader(),
    schema: docsSchema(),
  }),
};
