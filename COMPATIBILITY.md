Supported Features
==================

Here is a non-comprehensive table of git commands and features whose equivalent
is supported by go-git.

| Feature | Supported | Notes |
|---------|-----------|-------|
| **config** ||
| config | ✔ | Reading and modifying per-repository configuration (`.git/config`) is supported. Global configuration (`$HOME/.gitconfig`) is not. |
| **getting and creating repositories** ||
| init | ✔ | Plain init and `--bare` are supported. Flags `--template`, `--separate-git-dir` and `--shared` are not. |
| clone | ✔ | Plain clone and equivalents to `--progress`,  `--single-branch`, `--depth`, `--origin`, `--recurse-submodules` are supported. Others are not. | 
| **basic snapshotting** ||
| add | ✔ |
| status | ✔ |
| commit | ✔ |
| reset | ✔ |
| rm | ✖ |
| mv | ✖ |
| **branching and merging** ||
| branch | ✔ |
| checkout | ✔ |
| merge | ✖ |
| mergetool | ✖ |
| stash | ✖ |
| tag | ✔ |
| **sharing and updating projects** ||
| fetch | ✔ |
| pull | ✔ |
| push | ✔ |
| remote | ✔ |
| submodule | ✔ |
| **inspection and comparison** ||
| show | ✔ |
| log | ✔ |
| shortlog | (see log) |
| describe | |
| **patching** ||
| apply | ✖ |
| cherry-pick | ✖ |
| diff | ✖ |
| rebase | ✖ |
| revert | ✖ |
| **debugging** ||
| bisect | ✖ |
| blame | ✔ |
| grep | ✖ |
| **email** ||
| am | ✖ |
| apply | ✖ |
| format-patch | ✖ |
| send-email | ✖ |
| request-pull | ✖ |
| **external systems** ||
| svn | ✖ |
| fast-import | ✖ |
| **administration** ||
| clean | ✖ |
| gc | ✖ |
| fsck | ✖ |
| reflog | ✖ |
| filter-branch | ✖ |
| instaweb | ✖ |
| archive | ✖ |
| bundle | ✖ |
| prune | ✖ |
| repack | ✖ |
| **server admin** ||
| daemon | |
| update-server-info | |
| **advanced** ||
| notes | ✖ |
| replace | ✖ |
| worktree | ✖ |
| annotate | (see blame) |
| **gpg** ||
| git-verify-commit | ✖ |
| git-verify-tag | ✖ |
| **plumbing commands** ||
| cat-file | ✔ |
| check-ignore | |
| commit-tree | |
| count-objects | |
| diff-index | |
| for-each-ref | ✔ |
| hash-object | ✔ |
| ls-files | ✔ |
| merge-base | |
| read-tree | |
| rev-list | ✔ |
| rev-parse | |
| show-ref | ✔ |
| symbolic-ref | ✔ |
| update-index | |
| update-ref | |
| verify-pack | |
| write-tree | |
| **protocols** | |
| http(s):// (dumb) | ✖ |
| http(s):// (smart) | ✔ |
| git:// | ✔ |
| ssh:// | ✔ |
| file:// | ✔ |
| custom | ✔ |
| **other features** ||
| gitignore | ✖ |
| gitattributes | ✖ |
| index version | |
| packfile version | |
| push-certs | ✖ |
