/** The hub answered with an error. */
export class ApiError extends Error {
  override readonly name = "ApiError";
  readonly status: number;
  readonly code: string;
  /** What is wrong with which field of the request, by the field's SDK name. */
  readonly fields: Readonly<Record<string, string>> | undefined;

  constructor(
    status: number,
    code: string,
    message: string,
    fields?: Readonly<Record<string, string>>,
  ) {
    super(message);
    this.status = status;
    this.code = code;
    this.fields = fields;
  }
}

/** The hub did not answer, or not in a way that could be read. */
export class NetworkError extends Error {
  override readonly name = "NetworkError";

  constructor(message: string, cause?: unknown) {
    super(message, { cause });
  }
}

/** The gateway connection ended and will not be tried again. */
export class GatewayError extends Error {
  override readonly name = "GatewayError";
  /** The close code, if the hub gave one. */
  readonly code: number | undefined;

  constructor(message: string, code?: number, cause?: unknown) {
    super(message, { cause });
    this.code = code;
  }
}
