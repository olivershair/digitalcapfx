'use client'

import React, { useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { ArrowLeft, ArrowRight, ShieldCheck, Mail, Phone } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { OtpInput } from '@/components/ui/OtpInput'
import { Tabs } from '@/components/ui/Tabs'
import { PhoneInput } from '@/components/ui/PhoneInput'
import AuthLayout from '@/components/pages/no-auth/layout/AuthLayout'
import { useMutation } from '@tanstack/react-query'
import { authService } from '@/services/auth.service'
import { useFormErrors } from '@/hooks/useFormErrors'
import { toast } from 'sonner'
import { isValidPhoneNumber } from 'react-phone-number-input'
import { useLanguageStore } from '@/store/languageStore'
import { smsErrorMessage } from '@/lib/smsErrors'
import { apiErrorMessage } from '@/lib/apiErrors'
const ForgotPassword = () => {
    const { t, language } = useLanguageStore();
    const router = useRouter();
    const [activeTab, setActiveTab] = useState<'email' | 'phone'>('phone');
    const usePhone = activeTab === 'phone';
    const [email, setEmail] = useState('');
    const [phone, setPhone] = useState('');
    const [emailOrPhone, setEmailOrPhone] = useState('');
    const [code, setCode] = useState('');
    const [newPin, setNewPin] = useState('');
    const [step, setStep] = useState<'request' | 'reset'>('request');
    const { errors, setErrors, errorMsg, setErrorMsg, clearFieldError } = useFormErrors();

    const forgotPasswordTabs = [
        { id: 'phone', label: t('auth.forgot.tabPhone', { defaultValue: 'Phone number' }) },
        { id: 'email', label: t('auth.forgot.tabEmail', { defaultValue: 'Email address' }) },
    ];

    // React Query Mutations
    const forgotPinMutation = useMutation({
        mutationFn: (emailOrPhone: string) => authService.forgotPin(emailOrPhone),
        onSuccess: (data) => {
            if (data?.success) {
                toast.success(t('messages.reset.codeSent'));
                setStep('reset');
            } else {
                const msg = apiErrorMessage(data, 'errors.action.requestReset');
                setErrorMsg(msg);
                toast.error(msg);
            }
        },
        onError: (err: any, target: string) => {
            console.error('Forgot PIN Error:', err);
            const smsMsg = smsErrorMessage(err, target, t, language);
            if (smsMsg) {
                setErrorMsg(smsMsg);
                toast.error(smsMsg);
                return;
            }
            const msg = apiErrorMessage(err, 'errors.action.requestReset');
            setErrorMsg(msg);
            toast.error(msg);
        }
    });

    const resetPinMutation = useMutation({
        mutationFn: (payload: { emailOrPhone: string; code: string; newPin: string }) =>
            authService.resetPin(payload),
        onSuccess: (data) => {
            if (data?.success) {
                toast.success(t('messages.reset.success'));
                router.push('/login');
            } else {
                const msg = apiErrorMessage(data, 'errors.action.resetPin');
                setErrorMsg(msg);
                toast.error(msg);
            }
        },
        onError: (err: any) => {
            console.error('Reset PIN Error:', err);
            const msg = apiErrorMessage(err, 'errors.action.resetPin');
            setErrorMsg(msg);
            toast.error(msg);
        }
    });

    const handleRequestSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        setErrorMsg('');

        if (usePhone) {
            if (!phone) {
                setErrors({ phone: t('messages.validation.phoneRequired') });
                return;
            }
            if (!isValidPhoneNumber(phone)) {
                setErrors({ phone: t('messages.validation.phoneInvalid') });
                return;
            }
            setErrors({});
            setEmailOrPhone(phone);
            forgotPinMutation.mutate(phone);
        } else {
            const trimmedEmail = email.trim();
            if (!trimmedEmail) {
                setErrors({ email: t('messages.validation.emailRequired') });
                return;
            }
            const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
            if (!emailRegex.test(trimmedEmail)) {
                setErrors({ email: t('messages.validation.email') });
                return;
            }
            setErrors({});
            setEmailOrPhone(trimmedEmail);
            forgotPinMutation.mutate(trimmedEmail);
        }
    };

    const handleResetSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        setErrorMsg('');

        const newErrors: Record<string, string> = {};
        if (code.length !== 6) {
            newErrors.code = t('messages.validation.code6');
        }
        if (!newPin.trim()) {
            newErrors.newPin = t('messages.validation.pinRequired');
        } else if (newPin.length !== 6) {
            newErrors.newPin = t('messages.validation.pinLength');
        }

        setErrors(newErrors);
        if (Object.keys(newErrors).length > 0) return;

        resetPinMutation.mutate({
            emailOrPhone: emailOrPhone.trim(),
            code,
            newPin: newPin.trim(),
        });
    };

    return (
        <AuthLayout>
            {step === 'request' ? (
                /* Phase 1: Request OTP Code */
                <div className="space-y-6 text-left animate-in fade-in slide-in-from-bottom-2 duration-300">
                    <div>
                        <Link
                            href="/login"
                            className="inline-flex items-center space-x-2 text-xs font-bold text-slate-400 hover:text-cyan-300 transition-colors font-sans select-none"
                        >
                            <ArrowLeft className="h-4 w-4" />
                            <span>Back to sign in</span>
                        </Link>
                    </div>

                    <div className="relative w-fit">
                        <div className="absolute inset-0 rounded-2xl bg-cyan-500/20 blur-[20px]"></div>
                        <div className="relative w-12 h-12 rounded-2xl bg-cyan-500/10 border border-cyan-500/25 flex items-center justify-center text-cyan-400">
                            {usePhone ? <Phone className="h-5 w-5" /> : <Mail className="h-5 w-5" />}
                        </div>
                    </div>

                    <div className="space-y-2">
                        <h1 className="font-satoshi font-black text-3xl text-white tracking-tight">
                            Forgot your PIN?
                        </h1>
                        <p className="text-slate-400 text-sm font-sans leading-relaxed">
                            Enter the email address or phone number linked to your account and we'll send you a verification code.
                        </p>
                    </div>

                    {errorMsg && (
                        <div className="p-4 bg-rose-500/10 border border-rose-500/25 rounded-2xl text-xs font-semibold text-rose-400 font-sans leading-relaxed select-none">
                            {errorMsg}
                        </div>
                    )}

                    <form onSubmit={handleRequestSubmit} className="space-y-5">
                        <div className="w-full flex justify-center pb-1">
                            <Tabs
                                tabs={forgotPasswordTabs}
                                activeTab={activeTab}
                                onChange={(id) => {
                                    setActiveTab(id as 'email' | 'phone');
                                    setErrors({});
                                    setErrorMsg('');
                                }}
                                className="w-full grid grid-cols-2 text-center"
                            />
                        </div>

                        {usePhone ? (
                            <PhoneInput
                                required
                                label="Phone number*"
                                placeholder="Enter phone number"
                                value={phone}
                                onChange={(val) => {
                                    setPhone(val);
                                    clearFieldError('phone');
                                }}
                                error={errors.phone}
                            />
                        ) : (
                            <Input
                                required
                                type="email"
                                label="Email address*"
                                placeholder="you@email.com"
                                value={email}
                                onChange={(e) => {
                                    setEmail(e.target.value);
                                    clearFieldError('email');
                                }}
                                error={errors.email}
                            />
                        )}

                        <Button
                            type="submit"
                            variant="primary"
                            disabled={forgotPinMutation.isPending}
                            className="w-full rounded-xl h-[52px] font-bold text-base bg-gradient-to-r from-primary-600 via-primary-500 to-cyan-500 hover:from-primary-500 hover:to-cyan-400 text-white shadow-xl shadow-cyan-500/20 hover:shadow-cyan-500/35 transition-all duration-200 hover:scale-[1.01] active:scale-[0.99]"
                            rightIcon={<ArrowRight className="h-5 w-5" />}
                        >
                            {forgotPinMutation.isPending ? 'Sending...' : 'Send reset code'}
                        </Button>
                    </form>

                    <div className="text-center pt-2 select-none">
                        <span className="text-xs font-medium text-slate-400 font-sans">
                            Remembered it?{' '}
                            <Link href="/login" className="text-cyan-400 font-bold hover:text-cyan-300 hover:underline">
                                Sign in instead
                            </Link>
                        </span>
                    </div>
                </div>
            ) : (
                /* Phase 2: Verify OTP & Enter New PIN */
                <div className="space-y-6 text-left animate-in fade-in slide-in-from-bottom-2 duration-300">
                    <div>
                        <button
                            onClick={() => {
                                setStep('request');
                                setCode('');
                                setNewPin('');
                                setErrors({});
                                setErrorMsg('');
                            }}
                            className="inline-flex items-center space-x-2 text-xs font-bold text-slate-500 hover:text-slate-355 transition-colors font-sans select-none"
                        >
                            <ArrowLeft className="h-4 w-4" />
                            <span>Change email/phone</span>
                        </button>
                    </div>

                    <div className="relative w-fit">
                        <div className="absolute inset-0 rounded-2xl bg-cyan-500/20 blur-[20px]"></div>
                        <div className="relative w-12 h-12 rounded-2xl bg-cyan-500/10 border border-cyan-500/25 flex items-center justify-center text-cyan-400">
                            <ShieldCheck className="h-5 w-5" />
                        </div>
                    </div>

                    <div className="space-y-2">
                        <h1 className="font-satoshi font-black text-3xl text-white tracking-tight">
                            Reset your PIN
                        </h1>
                        <p className="text-[#6D778A] text-sm font-sans leading-relaxed">
                            A 6-digit code has been sent to your registered contact. Check your SMS or email, then enter the code below with your new PIN.
                        </p>
                    </div>

                    {errorMsg && (
                        <div className="p-4 bg-rose-500/10 border border-rose-500/25 rounded-2xl text-xs font-semibold text-rose-400 font-sans leading-relaxed select-none">
                            {errorMsg}
                        </div>
                    )}

                    <form onSubmit={handleResetSubmit} className="space-y-5">
                        <div className="space-y-1.5">
                            <label className="text-xs font-semibold text-slate-400 block tracking-wide select-none">
                                Verification Code*
                            </label>
                            <div className="max-w-[340px] flex py-1">
                                <OtpInput
                                    value={code}
                                    onChange={(val) => {
                                        setCode(val);
                                        clearFieldError('code');
                                    }}
                                    length={6}
                                />
                            </div>
                            {errors.code && (
                                <p className="text-xs text-red-500 font-semibold mt-1">
                                    {errors.code}
                                </p>
                            )}
                        </div>

                        <div className="space-y-1.5">
                            <Input
                                required
                                type="password"
                                label="New 6-Digit PIN*"
                                placeholder="Enter new 6-digit PIN"
                                maxLength={6}
                                value={newPin}
                                onChange={(e) => {
                                    const cleaned = e.target.value.replace(/\D/g, '');
                                    setNewPin(cleaned);
                                    clearFieldError('newPin');
                                }}
                                error={errors.newPin}
                            />
                            <span className="text-[10px] text-slate-500 font-semibold block select-none">
                                Pin code must be 6 numeric digits for rapid authentication.
                            </span>
                        </div>

                        <Button
                            type="submit"
                            variant="primary"
                            disabled={resetPinMutation.isPending}
                            className="w-full rounded-xl h-[52px] font-bold text-base bg-gradient-to-r from-primary-600 via-primary-500 to-cyan-500 hover:from-primary-500 hover:to-cyan-400 text-white shadow-xl shadow-cyan-500/20 hover:shadow-cyan-500/35 transition-all duration-200 hover:scale-[1.01] active:scale-[0.99]"
                            rightIcon={<ArrowRight className="h-5 w-5" />}
                        >
                            {resetPinMutation.isPending ? 'Resetting PIN...' : 'Reset PIN'}
                        </Button>
                    </form>
                </div>
            )}
        </AuthLayout>
    );
};

export default ForgotPassword;
