"use client";

import React, { useEffect } from "react";
import { X, AlertTriangle, AlertCircle, Info, CheckCircle2, Loader2 } from "lucide-react";
import { Button } from "./button";

interface ModalPopupProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  icon?: React.ReactNode;
  children: React.ReactNode;
  maxWidth?: string;
}

export function ModalPopup({
  isOpen,
  onClose,
  title,
  icon,
  children,
  maxWidth = "max-w-md",
}: ModalPopupProps) {
  useEffect(() => {
    if (!isOpen) return;
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* High-contrast solid dark backdrop without blur */}
      <div
        className="fixed inset-0 bg-black/80 transition-opacity"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Modal Dialog Card */}
      <div
        role="dialog"
        aria-modal="true"
        className={`relative z-10 w-full ${maxWidth} border border-zinc-800 bg-[#0e0e0e] text-zinc-200 shadow-2xl rounded-none`}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-zinc-800 bg-[#0a0a0a]">
          <div className="flex items-center gap-2">
            {icon}
            <h3 className="text-xs font-mono font-bold uppercase tracking-wider text-zinc-100">
              {title}
            </h3>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close dialog"
            className="p-1 text-zinc-500 hover:text-zinc-200 hover:bg-zinc-800 transition-colors"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-4">{children}</div>
      </div>
    </div>
  );
}

interface ConfirmModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  variant?: "danger" | "warning" | "info";
  loading?: boolean;
}

export function ConfirmModal({
  isOpen,
  onClose,
  onConfirm,
  title,
  message,
  confirmText = "Confirm",
  cancelText = "Cancel",
  variant = "danger",
  loading = false,
}: ConfirmModalProps) {
  const icon =
    variant === "danger" ? (
      <AlertTriangle className="h-4 w-4 text-rose-500" />
    ) : variant === "warning" ? (
      <AlertCircle className="h-4 w-4 text-amber-500" />
    ) : (
      <Info className="h-4 w-4 text-blue-500" />
    );

  const confirmBtnClass =
    variant === "danger"
      ? "bg-rose-600 hover:bg-rose-500 text-white"
      : variant === "warning"
      ? "bg-amber-600 hover:bg-amber-500 text-black font-semibold"
      : "bg-yellow-600 hover:bg-yellow-500 text-black font-semibold";

  return (
    <ModalPopup isOpen={isOpen} onClose={onClose} title={title} icon={icon}>
      <div className="space-y-4">
        <p className="text-xs text-zinc-300 leading-relaxed font-sans">{message}</p>

        <div className="flex items-center justify-end gap-2 pt-3 border-t border-zinc-850">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onClose}
            disabled={loading}
            className="h-8 text-xs rounded-none border-zinc-800 bg-zinc-900/40 text-zinc-400 hover:text-white hover:bg-zinc-800"
          >
            {cancelText}
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={onConfirm}
            disabled={loading}
            className={`h-8 text-xs rounded-none gap-1.5 ${confirmBtnClass}`}
          >
            {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {confirmText}
          </Button>
        </div>
      </div>
    </ModalPopup>
  );
}

interface AlertModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  message: string;
  details?: string;
  type?: "error" | "success" | "info";
}

export function AlertModal({
  isOpen,
  onClose,
  title,
  message,
  details,
  type = "error",
}: AlertModalProps) {
  const icon =
    type === "error" ? (
      <AlertCircle className="h-4 w-4 text-rose-500" />
    ) : type === "success" ? (
      <CheckCircle2 className="h-4 w-4 text-emerald-500" />
    ) : (
      <Info className="h-4 w-4 text-blue-500" />
    );

  return (
    <ModalPopup isOpen={isOpen} onClose={onClose} title={title} icon={icon}>
      <div className="space-y-4">
        <p className="text-xs text-zinc-300 leading-relaxed font-sans">{message}</p>

        {details && (
          <div className="p-2.5 bg-black border border-zinc-800 font-mono text-[11px] text-zinc-400 overflow-x-auto whitespace-pre-wrap break-all max-h-40">
            {details}
          </div>
        )}

        <div className="flex items-center justify-end pt-3 border-t border-zinc-850">
          <Button
            type="button"
            size="sm"
            onClick={onClose}
            className="h-8 px-4 text-xs rounded-none bg-zinc-800 hover:bg-zinc-700 text-zinc-100"
          >
            Acknowledge
          </Button>
        </div>
      </div>
    </ModalPopup>
  );
}
