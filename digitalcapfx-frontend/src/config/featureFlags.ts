/**
 * Global Feature Flags
 *
 * ALLOW_CRYPTO: Set to false to hide/disable all cryptocurrency options,
 * stablecoins (including iUSD), networks, and crypto trading across the
 * application. Fiat USD accounts remain available. Backend APIs are unaffected.
 */
export const FEATURE_FLAGS = {
    ALLOW_CRYPTO: false,
};

export const isCryptoEnabled = (): boolean => FEATURE_FLAGS.ALLOW_CRYPTO;

/**
 * List of crypto tokens/currencies hidden when ALLOW_CRYPTO is false.
 */
export const CRYPTO_CURRENCIES = [
    'IUSD',
    'USDT',
    'USDC',
    'BTC',
    'ETH',
    'SOL',
    'POL',
    'TRX',
    'BSC',
    'LTC',
    'XRP',
    'BCH',
    'BNB',
    'MATIC',
    'DOGE',
    'ADA',
    'AVAX',
    'DOT',
    'LINK',
    'UNI',
    'ATOM',
];

/**
 * Fiat USD is intentionally not listed: it remains available when crypto is hidden.
 */
export const isCryptoCurrency = (code?: string): boolean => {
    if (!code) return false;
    const upper = code.trim().toUpperCase();
    return CRYPTO_CURRENCIES.includes(upper);
};

export function isCryptoActivity(item: {
    asset?: string;
    currency?: string;
    source?: string;
    icon_type?: string;
    iconType?: string;
}): boolean {
    return isCryptoCurrency(item.asset || item.currency) ||
        (item.source || '').toLowerCase() === 'caas' ||
        (item.icon_type || item.iconType || '').toLowerCase().includes('crypto');
}

/**
 * Helper to filter an array of items containing currency codes based on ALLOW_CRYPTO flag.
 */
export function filterCryptoItems<T>(
    items: T[],
    getCode: (item: T) => string
): T[] {
    if (FEATURE_FLAGS.ALLOW_CRYPTO) return items;
    return items.filter((item) => !isCryptoCurrency(getCode(item)));
}
