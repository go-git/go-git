Supported Capabilities
======================

Here is a non-comprehensive table of git commands and features whose equivalent
is supported by go-git.

| Feature                               | Status | Sub-Feartures |Notes |
|---------------------------------------|--------|-------|-------|
| **config**                            |
| config                                | ✔ | ✔ Per-repository configuration (`.git/config`)<br/>✖ Global configuration (`$HOME/.gitconfig`)| |
| **getting and creating repositories** |
| init                                  | ✔ |  ✔ `--bare`<br/>✖ `--template`<br/>✖ `--separate-git-dir`<br/>✖ `--shared`| 
| clone                                 | ✔ |  ✔ `--progress`<br/>✔ `--single-branch`<br/>✔ `--depth`<br/>✔ `--origin`<br/>✔ `--recurse-submodules`| |
| **basic snapshotting** |
| add                                   | ✔ |  ✔ Plain add<br/>✖ Other flags| |
| status                                | ✔ |
| commit                                | ✔ |
| reset                                 | ✔ |
| rm                                    | ✔ |
| mv                                    | ✔ |
| **branching and merging** |
| branch                                | ✔ |
| checkout                              | ✔ | ✔ Basic usages | |
| merge                                 | ✖ |
| mergetool                             | ✖ |
| stash                                 | ✖ |
| tag                                   | ✔ |
| **sharing and updating projects** |
| fetch                                 | ✔ |
| pull                                  | ✔ | ✔ Fast-forward merge |  |
| push                                  | ✔ |
| remote                                | ✔ |
| submodule                             | ✔ |
| **inspection and comparison** |
| show                                  | ✔ |
| log                                   | ✔ |
| shortlog                              | (see log) |
| describe                              | |
| **patching** |
| apply                                 | ✖ |
| cherry-pick                           | ✖ |
| diff                                  | ✔ |  | Patch object with UnifiedDiff output representation |
| rebase                                | ✖ |
| revert                                | ✖ |
| **debugging** |
| bisect                                | ✖ |
| blame                                 | ✔ |
| grep                                  | ✔ |
| **email** ||
| am                                    | ✖ |
| apply                                 | ✖ |
| format-patch                          | ✖ |
| send-email                            | ✖ |
| request-pull                          | ✖ |
| **external systems** |
| svn                                   | ✖ |
| fast-import                           | ✖ |
| **administration** |
| clean                                 | ✔ |
| gc                                    | ✖ |
| fsck                                  | ✖ |
| reflog                                | ✖ |
| filter-branch                         | ✖ |
| instaweb                              | ✖ |
| archive                               | ✖ |
| bundle                                | ✖ |
| prune                                 | ✖ |
| repack                                | ✖ |
| **server admin** |
| daemon                                | |
| update-server-info                    | |
| **advanced** |
| notes                                 | ✖ |
| replace                               | ✖ |
| worktree                              | ✖ |
| annotate                              | (see blame) |
| **gpg** |
| git-verify-commit                     | ✔ |
| git-verify-tag                        | ✔ |
| **plumbing commands** |
| cat-file                              | ✔ |
| check-ignore                          | |
| commit-tree                           | |
| count-objects                         | |
| diff-index                            | |
| for-each-ref                          | ✔ |
| hash-object                           | ✔ |
| ls-files                              | ✔ |
| merge-base                            | ✔ |  ✔ `--independent`<br/>✔ `--is-ancestor`<br/>✖ `--fork-point`<br/>✖ `--octopus`| Calculates the merge-base only between two commits.|
| read-tree                             | |
| rev-list                              | ✔ |
| rev-parse                             | |
| show-ref                              | ✔ |
| symbolic-ref                          | ✔ |
| update-index                          | |
| update-ref                            | |
| verify-pack                           | |
| write-tree                            | |
| **protocols** |
| http(s):// (dumb)                     | ✖ |
| http(s):// (smart)                    | ✔ |
| git://                                | ✔ |
| ssh://                                | ✔ |
| file://                               | partial |  | Warning: this is not pure Golang. This shells out to the `git` binary. |
| custom                                | ✔ |
| **other features** |
| gitignore                             | ✔ |
| gitattributes                         | ✖ |
| index version                         | |
| packfile version                      | |
| push-certs                            | ✖ |
