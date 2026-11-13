#!/bin/sh
systemctl --global disable vantor-agent.service >/dev/null 2>&1 || true
