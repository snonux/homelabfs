/* SPDX-License-Identifier: GPL-2.0-only */
/* Substituted net/9p/protocol.c subset: little-endian 9P marshalling for the
 * formats the hfs exchange uses ("dbw" header, "bbqdB" TBRIDGE, "d" errors).
 * The fixed-tag lifetime under test does not depend on marshalling details.
 */
static int pdu_put(struct p9_fcall *pdu, const void *data, size_t len)
{
	if (pdu->offset + len > pdu->capacity)
		return -EFAULT;
	if (!len)
		return 0; /* FETCH carries an empty 'B' frame with a NULL pointer */
	memcpy(pdu->sdata + pdu->offset, data, len);
	pdu->offset += len;
	if (pdu->offset > pdu->size)
		pdu->size = pdu->offset;
	return 0;
}

static int pdu_get(struct p9_fcall *pdu, void *data, size_t len)
{
	if (pdu->offset + len > pdu->size)
		return -EFAULT;
	memcpy(data, pdu->sdata + pdu->offset, len);
	pdu->offset += len;
	return 0;
}

static void p9pdu_reset(struct p9_fcall *pdu)
{
	pdu->offset = 0;
	pdu->size = 0;
}

static int p9pdu_vwritef(struct p9_fcall *pdu, int proto_version, const char *fmt, va_list ap)
{
	int err = 0;

	for (; *fmt && !err; fmt++) {
		u8 b; u16 w; u32 d; u64 q;
		const void *data;

		switch (*fmt) {
		case 'b': b = va_arg(ap, int); err = pdu_put(pdu, &b, 1); break;
		case 'w': w = va_arg(ap, int); err = pdu_put(pdu, &w, 2); break;
		case 'd': d = va_arg(ap, u32); err = pdu_put(pdu, &d, 4); break;
		case 'q': q = va_arg(ap, u64); err = pdu_put(pdu, &q, 8); break;
		case 'B':
			d = va_arg(ap, u32);
			data = va_arg(ap, const void *);
			err = pdu_put(pdu, data, d);
			break;
		default: err = -EINVAL;
		}
	}
	return err;
}

static int p9pdu_writef(struct p9_fcall *pdu, int proto_version, const char *fmt, ...)
{
	va_list ap;
	int err;

	va_start(ap, fmt);
	err = p9pdu_vwritef(pdu, proto_version, fmt, ap);
	va_end(ap);
	return err;
}

static int p9pdu_prepare(struct p9_fcall *pdu, int16_t tag, int8_t type)
{
	pdu->id = type;
	return p9pdu_writef(pdu, 0, "dbw", 0, type, tag);
}

static int p9pdu_finalize(struct p9_client *clnt, struct p9_fcall *pdu)
{
	u32 size = pdu->size;

	memcpy(pdu->sdata, &size, 4);
	return 0;
}

static int p9pdu_readf(struct p9_fcall *pdu, int proto_version, const char *fmt, ...)
{
	va_list ap;
	int err = 0;

	va_start(ap, fmt);
	for (; *fmt && !err; fmt++) {
		switch (*fmt) {
		case 'b': err = pdu_get(pdu, va_arg(ap, s8 *), 1); break;
		case 'w': err = pdu_get(pdu, va_arg(ap, s16 *), 2); break;
		case 'd': err = pdu_get(pdu, va_arg(ap, s32 *), 4); break;
		default: err = -EINVAL; /* RERROR strings never reach this harness */
		}
	}
	va_end(ap);
	return err;
}
