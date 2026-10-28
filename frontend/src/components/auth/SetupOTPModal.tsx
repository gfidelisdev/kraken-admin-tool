"use client";

import React, { useState, useEffect } from "react";
import QRCode from "qrcode";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import {
  ShieldCheck,
  Smartphone,
  Copy,
  Check,
  AlertCircle,
  CheckCircle2,
  KeyRound,
  ShieldAlert,
} from "lucide-react";
import { getOTPSetup, verifyOTPSetup, disableOTP } from "@/lib/api";
import { useAuth } from "@/context/AuthContext";

interface SetupOTPModalProps {
  isOpen: boolean;
  onClose: () => void;
  isInitialPrompt?: boolean;
}

export function SetupOTPModal({ isOpen, onClose, isInitialPrompt = false }: SetupOTPModalProps) {
  const { user, refreshUser } = useAuth();
  const [loading, setLoading] = useState(false);
  const [qrCodeDataUrl, setQrCodeDataUrl] = useState<string | null>(null);
  const [secretKey, setSecretKey] = useState<string>("");
  const [authUrl, setAuthUrl] = useState<string>("");
  const [code, setCode] = useState("");
  const [isCopied, setIsCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [isVerifying, setIsVerifying] = useState(false);

  // Disable flow states
  const [showDisableConfirm, setShowDisableConfirm] = useState(false);
  const [disablePassword, setDisablePassword] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [isDisabling, setIsDisabling] = useState(false);

  useEffect(() => {
    if (!isOpen) {
      setCode("");
      setError(null);
      setSuccess(null);
      setShowDisableConfirm(false);
      setDisablePassword("");
      setDisableCode("");
      return;
    }

    if (!user?.otp_enabled) {
      loadSetupData();
    }
  }, [isOpen, user?.otp_enabled]);

  const loadSetupData = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await getOTPSetup();
      setSecretKey(res.secret);
      setAuthUrl(res.otpauth_url);

      const qr = await QRCode.toDataURL(res.otpauth_url, {
        width: 200,
        margin: 2,
        color: {
          dark: "#0f172a",
          light: "#ffffff",
        },
      });
      setQrCodeDataUrl(qr);
    } catch (err: any) {
      setError(err?.message || "Falha ao gerar segredo do autenticador.");
    } finally {
      setLoading(false);
    }
  };

  const handleCopySecret = () => {
    if (!secretKey) return;
    navigator.clipboard.writeText(secretKey);
    setIsCopied(true);
    setTimeout(() => setIsCopied(false), 2500);
  };

  const handleVerify = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!code || code.trim().length !== 6) {
      setError("Digite o código numérico de 6 dígitos.");
      return;
    }

    setError(null);
    setIsVerifying(true);
    try {
      const res = await verifyOTPSetup(code.trim());
      setSuccess(res.message || "Autenticador configurado e ativado com sucesso!");
      await refreshUser();
      if (typeof window !== "undefined") {
        sessionStorage.removeItem("kraken_otp_dismissed");
      }
    } catch (err: any) {
      setError(err?.message || "Código inválido. Verifique o horário do dispositivo e tente novamente.");
    } finally {
      setIsVerifying(false);
    }
  };

  const handleDisable = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!disablePassword) {
      setError("Sua senha atual é obrigatória para desativar o autenticador.");
      return;
    }

    setError(null);
    setIsDisabling(true);
    try {
      const res = await disableOTP(disablePassword, disableCode.trim());
      setSuccess(res.message || "Autenticador desativado com sucesso.");
      await refreshUser();
      setShowDisableConfirm(false);
      setDisablePassword("");
      setDisableCode("");
    } catch (err: any) {
      setError(err?.message || "Falha ao desativar autenticador.");
    } finally {
      setIsDisabling(false);
    }
  };

  const handleDismiss = () => {
    if (isInitialPrompt && typeof window !== "undefined") {
      sessionStorage.setItem("kraken_otp_dismissed", "true");
    }
    onClose();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleDismiss}
      title={user?.otp_enabled ? "Segurança: Autenticador (OTP)" : "Configurar Aplicativo Autenticador (OTP)"}
      description={
        user?.otp_enabled
          ? "Sua conta está protegida com autenticador. Você pode usar seu aplicativo para recuperar sua senha a qualquer momento."
          : "Vincule o Google Authenticator, Proton Authenticator ou similar para proteger seu acesso e recuperar sua senha."
      }
      maxWidth="md"
    >
      <div className="space-y-4">
        {error && (
          <div className="flex items-start space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
            <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
            <span>{error}</span>
          </div>
        )}

        {success && (
          <div className="flex items-start space-x-2.5 rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-3.5 text-xs text-emerald-300">
            <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400 mt-0.5" />
            <div>
              <p className="font-semibold">{success}</p>
              <p className="text-[11px] text-emerald-400/80 mt-1">
                Agora você pode redefinir sua senha na tela de login a qualquer momento usando apenas este aplicativo.
              </p>
            </div>
          </div>
        )}

        {user?.otp_enabled && !showDisableConfirm && (
          <div className="space-y-4">
            <div className="flex items-center gap-3 p-4 rounded-xl bg-emerald-500/10 border border-emerald-500/20">
              <div className="p-2 rounded-lg bg-emerald-500/20 text-emerald-400">
                <ShieldCheck className="h-6 w-6" />
              </div>
              <div>
                <h4 className="text-sm font-semibold text-white">Autenticador Ativo (2FA / OTP)</h4>
                <p className="text-xs text-slate-400 mt-0.5">
                  Google Authenticator, Proton Authenticator ou aplicativo compatível ativado para <span className="text-slate-200 font-mono">{user.username}</span>.
                </p>
              </div>
            </div>

            <div className="p-3.5 rounded-xl bg-slate-900 border border-slate-800 text-xs text-slate-300 space-y-2">
              <div className="flex items-center gap-2 text-sky-400 font-medium">
                <KeyRound className="h-4 w-4" />
                <span>Recuperação de Senha Habilitada</span>
              </div>
              <p className="text-slate-400 leading-relaxed">
                Caso você esqueça sua senha de acesso, você pode redefini-la instantaneamente na tela de login utilizando o código gerado no seu celular, sem necessidade de intervenção de outro administrador.
              </p>
            </div>

            <div className="flex items-center justify-between pt-2 border-t border-slate-800">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowDisableConfirm(true)}
                className="text-rose-400 border-rose-500/30 hover:bg-rose-500/10"
              >
                Desativar Autenticador
              </Button>
              <Button size="sm" onClick={handleDismiss}>
                Fechar
              </Button>
            </div>
          </div>
        )}

        {user?.otp_enabled && showDisableConfirm && (
          <form onSubmit={handleDisable} className="space-y-3.5">
            <div className="flex items-start gap-2.5 p-3 rounded-xl bg-rose-500/10 border border-rose-500/20 text-xs text-rose-300">
              <ShieldAlert className="h-4 w-4 shrink-0 text-rose-400 mt-0.5" />
              <span>
                Ao desativar o autenticador, você não poderá mais recuperar sua senha de forma independente usando seu aplicativo móvel.
              </span>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">Senha Atual</label>
              <Input
                type="password"
                placeholder="Digite sua senha de login"
                value={disablePassword}
                onChange={(e) => setDisablePassword(e.target.value)}
                required
                autoFocus
              />
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">Código de 6 dígitos do app (opcional)</label>
              <Input
                type="text"
                placeholder="Ex: 123456"
                maxLength={6}
                value={disableCode}
                onChange={(e) => setDisableCode(e.target.value)}
              />
            </div>

            <div className="flex justify-end gap-2 pt-2 border-t border-slate-800">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setShowDisableConfirm(false)}
                disabled={isDisabling}
              >
                Cancelar
              </Button>
              <Button
                type="submit"
                size="sm"
                className="bg-rose-600 hover:bg-rose-500 text-white"
                isLoading={isDisabling}
              >
                Confirmar Desativação
              </Button>
            </div>
          </form>
        )}

        {!user?.otp_enabled && !success && (
          <div className="space-y-4">
            {isInitialPrompt && (
              <div className="flex items-start gap-2.5 p-3 rounded-xl bg-sky-500/10 border border-sky-500/20 text-xs text-sky-300">
                <Smartphone className="h-4 w-4 shrink-0 text-sky-400 mt-0.5" />
                <span>
                  <strong>Aviso de Segurança:</strong> Sua conta ainda não possui um autenticador configurado. Recomendamos vincular seu aplicativo (Google Authenticator, Proton, Microsoft, etc.) para permitir a recuperação rápida de senha.
                </span>
              </div>
            )}

            {loading ? (
              <div className="py-8 text-center text-xs text-slate-400">
                Gerando chave criptográfica do autenticador...
              </div>
            ) : (
              <>
                <div className="flex flex-col sm:flex-row items-center gap-4 p-3.5 rounded-xl bg-slate-900 border border-slate-800">
                  {qrCodeDataUrl ? (
                    <div className="p-2 bg-white rounded-xl shrink-0 shadow-md">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={qrCodeDataUrl}
                        alt="QR Code Autenticador"
                        className="w-36 h-36"
                      />
                    </div>
                  ) : (
                    <div className="w-36 h-36 bg-slate-800 rounded-xl flex items-center justify-center text-xs text-slate-500">
                      QR Indisponível
                    </div>
                  )}

                  <div className="space-y-2 text-xs text-slate-300 flex-1">
                    <p className="font-semibold text-slate-200">1. Escaneie o QR Code</p>
                    <p className="text-slate-400 text-[11px] leading-relaxed">
                      Abra o <strong>Google Authenticator</strong>, <strong>Proton Authenticator</strong>, <strong>Authy</strong> ou outro aplicativo TOTP compatível e aponte a câmera.
                    </p>

                    <div className="pt-1">
                      <p className="text-[11px] text-slate-400 mb-1">Ou insira a chave manualmente:</p>
                      <div className="flex items-center gap-1.5">
                        <code className="bg-slate-950 border border-slate-800 px-2.5 py-1 rounded-lg text-sky-400 font-mono text-[11px] select-all break-all">
                          {secretKey}
                        </code>
                        <button
                          type="button"
                          onClick={handleCopySecret}
                          className="p-1 text-slate-400 hover:text-white rounded-md hover:bg-slate-800 transition"
                          title="Copiar Chave"
                        >
                          {isCopied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                        </button>
                      </div>
                    </div>
                  </div>
                </div>

                <form onSubmit={handleVerify} className="space-y-3">
                  <div>
                    <label className="text-xs font-semibold text-slate-300 block mb-1">
                      2. Digite o código de 6 dígitos gerado no app
                    </label>
                    <Input
                      type="text"
                      inputMode="numeric"
                      pattern="[0-9]*"
                      maxLength={6}
                      placeholder="Ex: 123456"
                      value={code}
                      onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
                      className="text-center font-mono tracking-widest text-lg font-bold"
                      autoFocus
                      required
                    />
                  </div>

                  <div className="flex items-center justify-between pt-2 border-t border-slate-800">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={handleDismiss}
                      className="text-slate-400 hover:text-white"
                    >
                      {isInitialPrompt ? "Lembrar mais tarde" : "Cancelar"}
                    </Button>
                    <Button
                      type="submit"
                      size="sm"
                      isLoading={isVerifying}
                      disabled={code.length !== 6}
                      className="bg-sky-600 hover:bg-sky-500 text-white shadow-lg shadow-sky-600/20"
                    >
                      Confirmar e Ativar
                    </Button>
                  </div>
                </form>
              </>
            )}
          </div>
        )}

        {success && (
          <div className="flex justify-end pt-2 border-t border-slate-800">
            <Button size="sm" onClick={handleDismiss}>
              Concluir
            </Button>
          </div>
        )}
      </div>
    </Modal>
  );
}
