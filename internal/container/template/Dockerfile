# The container of a Session Workbench Remote session: the app builds it,
# starts it and connects with ssh, then tmux, as on any agent host. Your
# project directory is mounted at the same path, and you are the same user
# (same user and group id) inside, so files keep their owner.
#
# Add what the project needs in the first part (the Dockerfile window's
# buttons add common toolchains). Keep the part marked "Needed by Session
# Workbench": the app relies on it.
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive LANG=C.UTF-8

# Needed by Session Workbench: your computer's extra certificate
# authorities (e.g. of a company proxy that inspects HTTPS), passed by the
# app as the build context sw-certs (empty without any). Installing
# ca-certificates below makes them trusted for every download after it.
COPY --from=sw-certs . /usr/local/share/ca-certificates/sw/
ENV NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt

# ---- The project's software ----
RUN apt-get update && apt-get install -y --no-install-recommends \
        openssh-server tmux git ca-certificates curl less nano sudo \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

# ---- Needed by Session Workbench (keep this part) ----
# The user: your name and ids (the app passes them); an existing user with
# the same id (ubuntu) makes way.
ARG SW_USER=dev
ARG SW_UID=1000
ARG SW_GID=1000
RUN set -e; \
    old="$(getent passwd "$SW_UID" | cut -d: -f1 || true)"; \
    if [ -n "$old" ] && [ "$old" != "$SW_USER" ]; then userdel -r "$old" 2>/dev/null || userdel "$old"; fi; \
    getent group "$SW_GID" >/dev/null || groupadd -g "$SW_GID" "$SW_USER"; \
    id "$SW_USER" >/dev/null 2>&1 || useradd -m -u "$SW_UID" -g "$SW_GID" -s /bin/bash "$SW_USER"; \
    mkdir -p /run/sshd "/home/$SW_USER/.claude"; \
    chown "$SW_UID:$SW_GID" "/home/$SW_USER/.claude"; \
    printf '%s\n' \
        'HostKey /etc/sw/ssh_host_ed25519_key' \
        'AuthorizedKeysFile /etc/sw/authorized_keys' \
        'PasswordAuthentication no' \
        'KbdInteractiveAuthentication no' \
        'PermitRootLogin no' \
        'AllowAgentForwarding no' \
        'AllowTcpForwarding no' \
        'X11Forwarding no' \
        "AllowUsers $SW_USER" > /etc/ssh/sshd_config.d/sw.conf; \
    printf '%s\n' 'export CLAUDE_CONFIG_DIR="$HOME/.claude"' \
        'export NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt' > /etc/profile.d/sw-claude.sh
# Claude Code for the user, in ~/.local/bin: your computer's own (Linux,
# the same version; passed by the app as the build context sw-tools),
# else from the official installer. Its login and settings live in
# ~/.claude, a volume that is kept when the image is rebuilt.
USER $SW_USER
RUN --mount=type=bind,from=sw-tools,target=/opt/sw-tools \
    mkdir -p "$HOME/.local/bin" && \
    if [ -x /opt/sw-tools/claude ]; then cp /opt/sw-tools/claude "$HOME/.local/bin/claude"; \
    else curl -fsSL https://claude.ai/install.sh -o /tmp/claude-install.sh && bash /tmp/claude-install.sh \
        && rm /tmp/claude-install.sh; fi && \
    test -x "$HOME/.local/bin/claude"
USER root
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
