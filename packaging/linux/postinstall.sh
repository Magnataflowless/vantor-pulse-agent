#!/bin/sh
# Liga o coletor para todos os usuários (abre no próximo login gráfico).
systemctl --global enable digivant-agent.service >/dev/null 2>&1 || true
echo "Digivant Pulse instalado. Matricule o computador: sudo digivant-agent enroll --server https://SEU-PAINEL --token CODIGO"
