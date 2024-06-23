#!/bin/bash

release=("badger"  "core"  "etcd"  "nuts"  "olric"  "otter"  "redis")

IFS= read -r -d '' tpl <<EOF
name: Tag submodules on release

on:
  create:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  tag-all-submodules:
    runs-on: ubuntu-latest
    name: Tag all submodules
    steps:
EOF
workflow+="$tpl"

for i in ${!release[@]}; do
  lower="${release[$i]}"
  capitalized="$(tr '[:lower:]' '[:upper:]' <<< ${lower:0:1})${lower:1}"
  IFS= read -d '' tpl <<EOF
      -
        name: Create $capitalized tag
        uses: actions/github-script@v7
        with:
          script: |
            github.rest.git.createRef({
              owner: context.repo.owner,
              repo: context.repo.repo,
              ref: 'refs/tags/$lower/\${{ github.ref_name }}',
              sha: context.sha
            })
      -
        name: Create $capitalized caddy tag
        uses: actions/github-script@v7
        with:
          script: |
            github.rest.git.createRef({
              owner: context.repo.owner,
              repo: context.repo.repo,
              ref: 'refs/tags/$lower/caddy/\${{ github.ref_name }}',
              sha: context.sha
            })
EOF
  workflow+="$tpl"
done
echo "${workflow%$'\n'}" >  "$( dirname -- "$0"; )/release.yml"