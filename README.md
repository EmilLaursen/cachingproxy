# Cachingproxy

Proxy server which caches responses. Cache is keyed on the request path and body.
Multipart/form-data requests are parsed before hitting the cache to ensure
distinct keys.