# Gemba external Buildroot tree.
#
# Buildroot discovers per-package .mk files under this dir via this
# include. Only one package today (gemba-vm-init); add more by dropping
# them under package/<name>/ and they'll be picked up automatically.

include $(sort $(wildcard $(BR2_EXTERNAL_GEMBA_PATH)/package/*/*.mk))
