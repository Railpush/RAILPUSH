FROM alpine:3.20

# Keep kubectl close to the cluster version to avoid API incompatibilities.
ARG KUBECTL_VERSION=v1.32.3

RUN apk add --no-cache ca-certificates curl zstd coreutils

RUN curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
  && chmod 0755 /usr/local/bin/kubectl

COPY deploy/images/tenant-backup/tenant-backup.sh /usr/local/bin/tenant-backup
RUN chmod 0755 /usr/local/bin/tenant-backup

# Match the nonroot distroless UID used elsewhere in the cluster.
RUN addgroup -S -g 65532 railpush \
  && adduser -S -D -H -u 65532 -G railpush railpush

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/tenant-backup"]
