# etcd-helper

This is a helper for setting up etcd clusters.

If you have at least one etcd node up, then you can use this helper to
add members to the cluster as part of the "normal" etcd startup. The
help will also start etcd in proxy mode if enough members are already
present in the cluster.

## Motivation

Using
[discovery](https://github.com/coreos/etcd/blob/master/Documentation/clustering.md#etcd-discovery)
is generally the perferred way to initialize a cluster.  However, I
have had issues with it and it requires knowing a URL ahead of time
and bootstrapping the cluster in a set amount of time.

`etcd-helper` is meant for a more "hands-off" approach to an etcd
cluster.

## Usage

`etcd-helper` only needs to talk to one etcd node in the cluster.
That node can be a proxy. You may need to bootstrap a single etcd node
for initial setup.


`etcd-helper` is entirely controlled by environment variables. Most of
these have the same meaning as in [etcd](https://github.com/coreos/etcd/blob/master/Documentation/configuration.md)

* ETCD_DATA_DIR - defaults to `/var/lib/etcd`
* ETCD_DISCOVERY - if this is set, `etcd-helper` will not attempt to
  autojoin a cluster, but will just set base environment variables and
  exec etcd
* ETCD_NAME - defaults to hostname.  Will truncate to the base
hostname with no domain information.
* ETCD_LISTEN_CLIENT_URLS - this will also set ETCD_ADVERTISE_URLS for
etcd
* ETCD_LISTEN_PEER_URLS

A few non-standard environment variables are used:

* ETCD_PATH - path to etcd binary. If unset, then it attempt to find
it in `$PATH`
* ETCD_PEERS - comma seperated list of other etcd nodes to use to
  attempt automatic cluster join/proxy.  It will try all of these in
  order. Note: this is the "client" url of the etcd nodes.
* ETCD_MEMBERS - maximum number of etcd nodes in the cluser. any nodes
  starte dusing the helper once this number is met will start in proxy
  mode. No attempt is made to automatically switch modes on member failure.


All other environment variables are ignored and are **not** passed
along to etcd.


`etcd-helper` will
- check if discovery is set. if it is, then start etcd
- first try to detect if either node or proxy data is contained in the
data directory. If this is found, then start etcd
- contact peers to get current list of members
- if the number of members is suffecient, then start in proxy mode
- other wise, add this node as a member and start etcd
