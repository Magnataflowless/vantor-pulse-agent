#!/bin/sh
# Liga o coletor para todos os usuários (abre no próximo login gráfico).
systemctl --global enable vantor-agent.service >/dev/null 2>&1 || true
echo "Vantor Pulse instalado. Matricule o computador: sudo vantor-agent enroll --server https://SEU-PAINEL --token CODIGO"
