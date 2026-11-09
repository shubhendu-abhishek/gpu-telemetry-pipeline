# GitHub setup and push

The local project is initialized with a Git history. Create the empty target repository under the desired owner before pushing.

```bash
git config user.name "Your Name"
git config user.email "your-verified-github-email@example.com"
git add .
git commit -m "feat: implement elastic GPU telemetry pipeline"

git remote add origin https://github.com/shubhendu-abhishek/gpu-telemetry-pipeline.git
git push -u origin main
```

If GitHub already initialized `main` with a starter README, do not force-push. Create a feature branch from the local complete project, fetch `origin/main`, merge with `--allow-unrelated-histories`, resolve the README add/add conflict keeping the project README, commit and push the feature branch. Then create a PR into `main`.

Authenticate using Git Credential Manager or `gh auth login`; never place a PAT in the remote URL or repository. Protect `main` and require CI before merging.
