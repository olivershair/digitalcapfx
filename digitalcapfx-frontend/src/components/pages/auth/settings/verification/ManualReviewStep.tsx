'use client'

import React from 'react'
import { ClipboardCheck, CheckCircle2, Circle, ArrowLeft, Send, AlertTriangle, ShieldCheck } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/Button'
import { DocumentSpec } from '@/services/kyc.service'
import { useLanguageStore } from '@/store/languageStore'

interface ManualReviewStepProps {
  stepNumber: number;
  /** Documents the customer can upload (already filtered by the parent). */
  documents: DocumentSpec[];
  /** doc key → stored reference, for the ones already uploaded. */
  uploadedDocUrls: Record<string, string>;
  intakeSubmitted: boolean;
  isSubmitting: boolean;
  /** Labels returned by the backend when it refused the submission. */
  missingFromServer: string[];
  onSubmit: () => void;
  onBack: () => void;
}

/**
 * Last step when KYC runs in manual mode (KYC_PROVIDER=manual): instead of a
 * vendor's biometric check, the customer reviews the checklist and submits
 * the file; the compliance team approves or rejects it.
 */
export const ManualReviewStep: React.FC<ManualReviewStepProps> = ({
  stepNumber,
  documents,
  uploadedDocUrls,
  intakeSubmitted,
  isSubmitting,
  missingFromServer,
  onSubmit,
  onBack,
}) => {
  const { t } = useLanguageStore();

  const required = documents.filter((d) => d.required);
  const missingRequired = required.filter((d) => !uploadedDocUrls[d.key]);
  const canSubmit = intakeSubmitted && missingRequired.length === 0 && !isSubmitting;

  const checklist = [
    { key: '__intake', label: t('settings.verification.manual.checkIntake'), done: intakeSubmitted, required: true },
    ...documents.map((d) => ({ key: d.key, label: d.label, done: Boolean(uploadedDocUrls[d.key]), required: Boolean(d.required) })),
  ];

  return (
    <div className="space-y-6 w-full animate-in fade-in duration-300">
      <div className="bg-[#0C1224] border border-[#131B30] rounded-3xl p-6 md:p-8 space-y-6 text-left shadow-xl">
        <div className="flex justify-between items-start border-b border-white/[0.04] pb-4">
          <div className="space-y-1">
            <div className="flex items-center space-x-2">
              <ClipboardCheck className="h-5 w-5 text-emerald-400" />
              <h4 className="text-base font-bold text-white">
                {t('settings.verification.manual.title', { step: stepNumber })}
              </h4>
            </div>
            <p className="text-slate-400 text-xs leading-relaxed">
              {t('settings.verification.manual.desc')}
            </p>
          </div>
        </div>

        {/* Checklist */}
        <ul className="space-y-2.5">
          {checklist.map((item) => (
            <li
              key={item.key}
              className={cn(
                'flex items-center justify-between rounded-2xl border px-4 py-3',
                item.done ? 'border-emerald-500/20 bg-emerald-500/5' : 'border-white/10 bg-black/30'
              )}
            >
              <div className="flex items-center space-x-3 min-w-0">
                {item.done ? (
                  <CheckCircle2 className="h-4 w-4 text-emerald-400 shrink-0" />
                ) : (
                  <Circle className="h-4 w-4 text-slate-500 shrink-0" />
                )}
                <span className={cn('text-xs font-semibold break-words', item.done ? 'text-white' : 'text-slate-300')}>
                  {item.label}
                </span>
              </div>
              <span
                className={cn(
                  'text-[9px] font-extrabold uppercase px-2 py-0.5 rounded font-mono shrink-0 ml-3',
                  item.done
                    ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                    : item.required
                    ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                    : 'bg-slate-500/10 text-slate-400 border border-slate-500/20'
                )}
              >
                {item.done
                  ? t('settings.verification.manual.done')
                  : item.required
                  ? t('settings.verification.step2.required')
                  : t('settings.verification.step2.optional')}
              </span>
            </li>
          ))}
        </ul>

        {/* Errors / guidance */}
        {missingFromServer.length > 0 && (
          <div className="flex items-start space-x-3 rounded-2xl border border-amber-500/30 bg-amber-500/10 p-4">
            <AlertTriangle className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
            <div className="text-xs text-amber-200 space-y-1">
              <p className="font-bold">{t('settings.verification.manual.missingTitle')}</p>
              <p>{missingFromServer.join(', ')}</p>
            </div>
          </div>
        )}
        {!intakeSubmitted && (
          <p className="text-xs text-amber-300">{t('settings.verification.manual.intakeFirst')}</p>
        )}

        <div className="flex items-start space-x-3 rounded-2xl border border-white/10 bg-black/20 p-4">
          <ShieldCheck className="h-4 w-4 text-cyan-400 shrink-0 mt-0.5" />
          <p className="text-[11px] text-slate-400 leading-relaxed">{t('settings.verification.manual.privacy')}</p>
        </div>

        {/* Footer */}
        <div className="flex justify-between items-center pt-6 border-t border-white/[0.04]">
          <Button
            type="button"
            variant="secondary"
            onClick={onBack}
            className="rounded-xl text-xs font-bold px-5 h-11"
            leftIcon={<ArrowLeft className="h-4 w-4" />}
          >
            {t('settings.verification.manual.back')}
          </Button>

          <Button
            type="button"
            onClick={onSubmit}
            disabled={!canSubmit}
            className="rounded-xl text-xs font-bold px-6 h-11 bg-emerald-500 hover:bg-emerald-400 text-white disabled:opacity-50"
            rightIcon={<Send className="h-4 w-4" />}
          >
            {isSubmitting ? t('settings.verification.manual.submitting') : t('settings.verification.manual.submit')}
          </Button>
        </div>
      </div>
    </div>
  );
};
