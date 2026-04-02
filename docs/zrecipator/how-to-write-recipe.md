# How to Write a Recipe

## Links
- GUI with the new recipe system: https://app.zerops.io
> The GUI uses the production backend and data, so be careful — e.g., make sure you're not logged in as a user.
- Strapi recipe administration: https://api.zerops.io/admin/content-manager/collection-types/api::recipe.recipe
> Cache:
> Recipes use cache for data pulled from GitHub repositories. For changes to take effect immediately after a push, you need to click "Refresh Cache" in the recipe detail in Strapi.


## Procedure
0. Read through similar recipes, both in the GUI and in source code
    - Pay attention to the structure; the important files are: `README.md`, `zerops.yaml`, `import.yaml`
1. App structure in [https://github.com/zerops-recipe-apps](https://github.com/zerops-recipe-apps):
	1. [Create a new repo](https://github.com/organizations/zerops-recipe-apps/repositories/new) → name it `bun-hello-world-app`
		- Check the "Public template" checkbox in "Settings"
	2. Clone the old one and change its remote
	```bash
	git clone git@github.com:zeropsio/recipe-bun.git
    mv recipe-bun bun-hello-world-app
	cd bun-hello-world-app
	git remote set-url origin git@github.com:zerops-recipe-apps/bun-hello-world-app.git
	git push -u origin main
	```
	3. Remove `import.yaml`
		- Parts of it can optionally be used in `recipes/bun-hello-world/import.yaml`, see below
	4. Add app extract fragments[^1] to `README.md`
	5. Edit and comment the `zerops.yaml`
2. Copy `_template` or `_template_oss` in https://github.com/zeropsio/recipes and replace using full-text search:
	- `PLACEHOLDER_PROJECT_NAME` → `go-hello-world`
	- `PLACEHOLDER_PRETTY_RECIPE_NAME` → `Go Hello World`
	- `PLACEHOLDER_RECIPE_DIRECTORY` → `go-hello-world`
	- `PLACEHOLDER_RECIPE_SOFTWARE` → `[Go](https://go.dev) applications`
	- `PLACEHOLDER_RECIPE_DESCRIPTION` → `Simple Go API with single endpoint that reads from and writes to a PostgreSQL database.`
	- `PLACEHOLDER_COVER_SVG` → `cover-go.svg`
	- `PLACEHOLDER_RECIPE_TAGS` → `golang,echo`
	- `PLACEHOLDER_PRETTY_RECIPE_TAGS` → `Go`
3. Create and comment `import.yaml` for all environments
4. [Add the recipe to Strapi](https://api.zerops.io/admin/content-manager/collection-types/api::recipe.recipe/create)
5. Test launching all environments:
	- Dev services have the relevant technology commands available (`go`, `bun`, `cargo`, …)
	- Verify that the applications do what they're supposed to

## Result
- A recipe in the Strapi admin with a logo and correct categories
- A folder with individual environments in the [recipes repo](https://github.com/zeropsio/recipes)
	- For OSS recipes (Umami, ELK, Strapi, …): Development, Production
	- For languages and frameworks: AI Agent, Remote (CDE), Local, Stage, Small Production, Highly-Available Production
	- Each environment contains:
		- A documented `import.yaml`
		- A `README.md` with an environment description in the `intro` fragment[^1]
	- A main `README.md` with a recipe description in the `intro` fragment[^1]
- Repositories for each related app (e.g. https://github.com/zerops-recipe-apps/bun-hello-world-app)
	- Documented `zerops.yaml`
	- Updated `README.md` with extract fragments[^1]
	- A README that makes sense when visiting the app's GitHub repo

## Notes
- Documenting YAMLs serves both to explain things to users and, perhaps even more importantly, to instruct and train LLMs in the future
	- Include implementation notes in YAML comments, i.e. every part of the YAML that isn't truly obvious should have a description of why it's there and how it works

[^1]: The recipe system can only work with marked sections of `README.md` files, known as fragments.

    Fragment example in `README.md`:
    ```
    Lorem Ipsum ...

    <!-- #ZEROPS_EXTRACT_START:intro# -->
    **AI agent** environment provides a development space for AI agents to build and version the app.
    Comes with a dev service with the source code and necessary development tools, a staging service, email & SMTP testing tool, and a low-resource databases and storage.
    <!-- #ZEROPS_EXTRACT_END:intro# -->

    ... Lorem Ipsum
    ```

    Sections between `ZEROPS_EXTRACT_START` and `ZEROPS_EXTRACT_END` with a given key (in the example `intro`) are extracted and made available in the recipe system.

    Supported keys:

    | Key | Description | `README.md` Location |
    | --- | --- | --- |
    | `intro` | Intro/description of the given entity depending on which `README.md` it's in. | app, recipe, recipe environment |
    | `knowledge-base` | Useful information specific to the given software, framework, language, or recipe as a whole. | anywhere |
    | `integration-guide` | Guide on modifying the application to run on Zerops, including a documented `zerops.yaml`. | app of a framework |
    | `maintenance-guide` | Guides on operating and maintaining the application or the entire recipe on Zerops. | app of an OSS, OSS recipe environment |
