import React, { useId } from 'react';
import clsx from 'clsx';
import styles from './TextField.module.css';

export interface TextFieldProps
  extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  helperText?: string;
  errorText?: string;
  fullWidth?: boolean;
}

export const TextField = React.forwardRef<HTMLInputElement, TextFieldProps>(
  (
    {
      label,
      helperText,
      errorText,
      fullWidth = false,
      id: customId,
      disabled,
      className,
      ...rest
    },
    ref
  ) => {
    const generatedId = useId();
    const id = customId || generatedId;
    const errorId = `${id}-error`;
    const helperId = `${id}-helper`;

    const hasError = Boolean(errorText);

    return (
      <div
        className={clsx(
          styles.container,
          [fullWidth && styles.fullWidth, disabled && styles.disabled],
          className
        )}
      >
        {label && (
          <label htmlFor={id} className={styles.label}>
            {label}
          </label>
        )}

        <div className={styles.inputWrapper}>
          <input
            ref={ref}
            id={id}
            disabled={disabled}
            aria-invalid={hasError}
            aria-describedby={
              hasError ? errorId : helperText ? helperId : undefined
            }
            className={clsx(styles.input, hasError && styles.hasError)}
            {...rest}
          />
        </div>

        {hasError && (
          <p id={errorId} className={styles.errorText} role="alert">
            {errorText}
          </p>
        )}

        {!hasError && helperText && (
          <p id={helperId} className={styles.helperText}>
            {helperText}
          </p>
        )}
      </div>
    );
  }
);

TextField.displayName = 'TextField';
