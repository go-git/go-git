// Package rad implements a read-only rad:// transport for Radicle
// (https://radicle.xyz/) repositories stored on the local machine.
//
// # URL form
//
// A rad URL is rad://<rid>[/<nid>]. It has a host and at most one path
// segment, matching heartwood's Url::from_str:
//
//   - rid is the Radicle repository id in canonical form, for example
//     "z2cK19PnX6cAUgnZfMfwECBNppJ6z". It selects the storage repository at
//     $RAD_HOME/storage/<rid>.
//   - nid, if present, is a Radicle node id, that is, a peer's public key.
//     For example "z6Mkmywxg7Z7e63rkLTx7UJZeH69DLZ9KvhdQEPJ6N9nwVde". It
//     selects that peer's namespaced refs (refs/namespaces/<nid>/*) instead
//     of the repository's canonical refs.
//
// $RAD_HOME defaults to $HOME/.radicle, or $USERPROFILE/.radicle on
// Windows. This matches radicle::profile::home(). Set Options.Home to point
// at a different location, for example in tests.
//
// # What is served
//
// Without a namespace, the transport advertises the repository's canonical
// references: HEAD, refs/heads/* and refs/tags/*. It hides refs/rad/*,
// which holds Radicle's identity document and per-peer signed refs, and
// refs/namespaces/*, which holds every peer's copy of their own refs.
// Without this filter, cloning a repository with many peers would pull in
// every namespaced ref alongside the canonical ones. Set Options.AllRefs to
// turn the filter off and advertise everything, which is what raw
// git-upload-pack does, and what Radicle's own libgit2 local transport
// does. The view stays read-only either way.
//
// With a namespace (rad://<rid>/<nid>), the transport advertises only that
// peer's refs. They are read from refs/namespaces/<nid>/* with the prefix
// stripped, which is the equivalent of running git with GIT_NAMESPACE=<nid>
// set. Radicle namespaces have no HEAD of their own, so this mode
// advertises no HEAD. This matches git-remote-rad's list::for_fetch.
//
// The transport is read-only, and there is no option to change that.
// git-receive-pack is rejected with transport.ErrCommandUnsupported, and
// the storer passed to the pack protocol refuses reference writes on its
// own. Pushing raw refs would leave Radicle storage inconsistent until
// refs/rad/sigrefs is signed again with the device key, which this
// transport does not do. Use rad or git-remote-rad to publish.
// git-upload-archive is rejected too, because it needs a worktree-like view
// that Radicle storage does not advertise.
//
// # Prerequisite: rad seed, not rad clone
//
// This transport only reads from local storage. It never talks to the
// Radicle network. A repository is fetched from the network over the
// Radicle wire protocol, which runs node to node over Noise. That is what
// rad seed <rid> does: it updates the seeding policy and fetches the
// repository into $RAD_HOME/storage. rad clone is rad seed followed by a
// plain git clone of rad://, and this transport implements only that second
// half:
//
//	rad seed <rid>              // policy + network fetch into local storage
//	go-git clone rad://<rid>    // this transport, purely local
//
// If the repository is not in local storage yet, Load returns
// transport.ErrRepositoryNotFound wrapped in a message naming the
// rad seed <rid> command that fixes it.
//
// # Registration
//
// rad is not one of the client package's built-in schemes. It relies on
// Radicle-specific conventions that do not belong in go-git core, and it
// needs nothing beyond the standard library and go-git itself. Enable it
// explicitly with client.WithTransport:
//
//	r, err := git.PlainClone(dir, &git.CloneOptions{
//		URL: "rad://z2cK19PnX6cAUgnZfMfwECBNppJ6z",
//		ClientOptions: []client.Option{
//			client.WithTransport("rad", rad.NewTransport(rad.Options{})),
//		},
//	})
//
// # Known deviations from git-remote-rad
//
// These are follow-up work, not defects:
//
//   - HEAD comes from the storage repository's own HEAD file, usually
//     "ref: refs/heads/main". It is not resolved from the identity
//     document's default branch across delegate sigrefs. Raw
//     git-upload-pack and Radicle's own libgit2 local transport behave the
//     same way.
//   - Patch refs (refs/heads/patches/<id>) are not advertised.
//     git-remote-rad builds them from Radicle's collaborative objects
//     (COBs).
//   - There is no node interaction: no rad sync, no announce, no seeding,
//     and no fetch on demand over the control socket. A fetch only sees
//     what is already in local storage.
//   - This is not a substitute for an HTTPS seed. Radicle seed nodes run
//     radicle-httpd, which serves smart git over HTTP at
//     https://<seed>/<rid>.git. go-git can already clone that with the
//     plain http transport and no extra code, but without peer namespaces
//     and without sigrefs verification. Pick whichever fits: this transport
//     for local storage the Radicle node has already fetched, HTTPS for a
//     seed you do not run yourself.
package rad
