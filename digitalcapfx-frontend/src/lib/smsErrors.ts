import { parsePhoneNumber } from 'react-phone-number-input'
import en from 'react-phone-number-input/locale/en.json'
import fr from 'react-phone-number-input/locale/fr.json'
import es from 'react-phone-number-input/locale/es.json'

const COUNTRY_LABELS: Record<string, Record<string, string>> = { en, fr, es }

type Translate = (key: string, options?: Record<string, unknown>) => string

/**
 * Turns the backend's SMS errors into an explicit, translated message:
 *  - SMS_DESTINATION_UNSUPPORTED (422): this country / number cannot receive SMS
 *  - SMS_UNAVAILABLE (503): provider temporarily down — retry later
 * Returns null for any other error.
 */
export function smsErrorMessage(err: any, phone: string, t: Translate, language: string): string | null {
  const data = err?.response?.data;
  const code = data?.error?.code;
  if (code === 'SMS_UNAVAILABLE') {
    return t('auth.sms.unavailable');
  }
  if (code !== 'SMS_DESTINATION_UNSUPPORTED') {
    return null;
  }

  const reason = data?.data?.reason;
  const callingCode = data?.data?.callingCode || data?.data?.calling_code;
  let country = '';
  try {
    const iso = parsePhoneNumber(phone)?.country;
    const labels = COUNTRY_LABELS[(language || 'fr').slice(0, 2)] || COUNTRY_LABELS.en;
    country = iso ? labels[iso] || iso : '';
  } catch {
    country = '';
  }

  if (reason === 'country_not_supported') {
    return t('auth.sms.countryUnsupported', {
      country: country || t('auth.sms.thisCountry'),
      code: callingCode ? `+${callingCode}` : '',
    });
  }
  return t('auth.sms.numberRejected');
}
