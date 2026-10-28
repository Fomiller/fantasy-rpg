class_name GameV1

#
# BSD 3-Clause License
#
# Copyright (c) 2018 - 2026, Oleg Malyavkin
# All rights reserved.
#
# Redistribution and use in source and binary forms, with or without
# modification, are permitted provided that the following conditions are met:
#
# * Redistributions of source code must retain the above copyright notice, this
#   list of conditions and the following disclaimer.
#
# * Redistributions in binary form must reproduce the above copyright notice,
#   this list of conditions and the following disclaimer in the documentation
#   and/or other materials provided with the distribution.
#
# * Neither the name of the copyright holder nor the names of its
#   contributors may be used to endorse or promote products derived from
#   this software without specific prior written permission.
#
# THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
# AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
# IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
# DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
# FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
# DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
# SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
# CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
# OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
# OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

# DEBUG_TAB redefine this "  " if you need, example: const DEBUG_TAB = "\t"

const PROTO_VERSION = 3

const DEBUG_TAB : String = "  "

enum PB_ERR {
	NO_ERRORS = 0,
	VARINT_NOT_FOUND = -1,
	REPEATED_COUNT_NOT_FOUND = -2,
	REPEATED_COUNT_MISMATCH = -3,
	LENGTHDEL_SIZE_NOT_FOUND = -4,
	LENGTHDEL_SIZE_MISMATCH = -5,
	PACKAGE_SIZE_MISMATCH = -6,
	UNDEFINED_STATE = -7,
	PARSE_INCOMPLETE = -8,
	REQUIRED_FIELDS = -9
}

enum PB_DATA_TYPE {
	INT32 = 0,
	SINT32 = 1,
	UINT32 = 2,
	INT64 = 3,
	SINT64 = 4,
	UINT64 = 5,
	BOOL = 6,
	ENUM = 7,
	FIXED32 = 8,
	SFIXED32 = 9,
	FLOAT = 10,
	FIXED64 = 11,
	SFIXED64 = 12,
	DOUBLE = 13,
	STRING = 14,
	BYTES = 15,
	MESSAGE = 16,
	MAP = 17
}

const DEFAULT_VALUES_2 = {
	PB_DATA_TYPE.INT32: null,
	PB_DATA_TYPE.SINT32: null,
	PB_DATA_TYPE.UINT32: null,
	PB_DATA_TYPE.INT64: null,
	PB_DATA_TYPE.SINT64: null,
	PB_DATA_TYPE.UINT64: null,
	PB_DATA_TYPE.BOOL: null,
	PB_DATA_TYPE.ENUM: null,
	PB_DATA_TYPE.FIXED32: null,
	PB_DATA_TYPE.SFIXED32: null,
	PB_DATA_TYPE.FLOAT: null,
	PB_DATA_TYPE.FIXED64: null,
	PB_DATA_TYPE.SFIXED64: null,
	PB_DATA_TYPE.DOUBLE: null,
	PB_DATA_TYPE.STRING: null,
	PB_DATA_TYPE.BYTES: null,
	PB_DATA_TYPE.MESSAGE: null,
	PB_DATA_TYPE.MAP: null
}

const DEFAULT_VALUES_3 = {
	PB_DATA_TYPE.INT32: 0,
	PB_DATA_TYPE.SINT32: 0,
	PB_DATA_TYPE.UINT32: 0,
	PB_DATA_TYPE.INT64: 0,
	PB_DATA_TYPE.SINT64: 0,
	PB_DATA_TYPE.UINT64: 0,
	PB_DATA_TYPE.BOOL: false,
	PB_DATA_TYPE.ENUM: 0,
	PB_DATA_TYPE.FIXED32: 0,
	PB_DATA_TYPE.SFIXED32: 0,
	PB_DATA_TYPE.FLOAT: 0.0,
	PB_DATA_TYPE.FIXED64: 0,
	PB_DATA_TYPE.SFIXED64: 0,
	PB_DATA_TYPE.DOUBLE: 0.0,
	PB_DATA_TYPE.STRING: "",
	PB_DATA_TYPE.BYTES: [],
	PB_DATA_TYPE.MESSAGE: null,
	PB_DATA_TYPE.MAP: []
}

enum PB_TYPE {
	VARINT = 0,
	FIX64 = 1,
	LENGTHDEL = 2,
	STARTGROUP = 3,
	ENDGROUP = 4,
	FIX32 = 5,
	UNDEFINED = 8
}

enum PB_RULE {
	OPTIONAL = 0,
	REQUIRED = 1,
	REPEATED = 2,
	RESERVED = 3
}

enum PB_SERVICE_STATE {
	FILLED = 0,
	UNFILLED = 1
}

class PBField:
	func _init(a_name : String, a_type : int, a_rule : int, a_tag : int, packed : bool, a_value = null):
		name = a_name
		type = a_type
		rule = a_rule
		tag = a_tag
		option_packed = packed
		value = a_value
		
	var name : String
	var type : int
	var rule : int
	var tag : int
	var option_packed : bool
	var value
	var is_map_field : bool = false
	var option_default : bool = false

class PBTypeTag:
	var ok : bool = false
	var type : int
	var tag : int
	var offset : int

class PBServiceField:
	var field : PBField
	var func_ref = null
	var state : int = PB_SERVICE_STATE.UNFILLED

class PBPacker:
	static func convert_signed(n : int) -> int:
		if n < -2147483648:
			return (n << 1) ^ (n >> 63)
		else:
			return (n << 1) ^ (n >> 31)

	static func deconvert_signed(n : int) -> int:
		if n & 0x01:
			return ~(n >> 1)
		else:
			return (n >> 1)

	static func pack_varint(value) -> PackedByteArray:
		var varint : PackedByteArray = PackedByteArray()
		if typeof(value) == TYPE_BOOL:
			if value:
				value = 1
			else:
				value = 0
		for _i in range(9):
			var b = value & 0x7F
			value >>= 7
			if value:
				varint.append(b | 0x80)
			else:
				varint.append(b)
				break
		if varint.size() == 9 && (varint[8] & 0x80 != 0):
			varint.append(0x01)
		return varint

	static func pack_bytes(value, count : int, data_type : int) -> PackedByteArray:
		var bytes : PackedByteArray = PackedByteArray()
		if data_type == PB_DATA_TYPE.FLOAT:
			var spb : StreamPeerBuffer = StreamPeerBuffer.new()
			spb.put_float(value)
			bytes = spb.get_data_array()
		elif data_type == PB_DATA_TYPE.DOUBLE:
			var spb : StreamPeerBuffer = StreamPeerBuffer.new()
			spb.put_double(value)
			bytes = spb.get_data_array()
		else:
			for _i in range(count):
				bytes.append(value & 0xFF)
				value >>= 8
		return bytes

	static func unpack_bytes(bytes : PackedByteArray, index : int, count : int, data_type : int):
		if data_type == PB_DATA_TYPE.FLOAT:
			return bytes.decode_float(index)
		elif data_type == PB_DATA_TYPE.DOUBLE:
			return bytes.decode_double(index)
		elif data_type == PB_DATA_TYPE.FIXED32:
			return bytes.decode_u32(index)
		elif data_type == PB_DATA_TYPE.SFIXED32:
			return bytes.decode_s32(index)
		elif data_type == PB_DATA_TYPE.FIXED64:
			return bytes.decode_u64(index)
		elif data_type == PB_DATA_TYPE.SFIXED64:
			return bytes.decode_s64(index)
		else:
			var value : int = 0
			for i in range(count):
				value |= bytes[index + i] << (8 * i)
			return value

	static func unpack_varint(varint_bytes) -> int:
		var value : int = 0
		var i: int = varint_bytes.size() - 1
		while i > -1:
			value = (value << 7) | (varint_bytes[i] & 0x7F)
			i -= 1
		return value

	static func pack_type_tag(type : int, tag : int) -> PackedByteArray:
		return pack_varint((tag << 3) | type)

	static func isolate_varint(bytes : PackedByteArray, index : int) -> PackedByteArray:
		var i: int = index
		while i <= index + 10 && i < bytes.size(): # Protobuf varint max size is 10 bytes
			if !(bytes[i] & 0x80):
				return bytes.slice(index, i + 1)
			i += 1
		return [] # Unreachable

	static func unpack_type_tag(bytes : PackedByteArray, index : int) -> PBTypeTag:
		var varint_bytes : PackedByteArray = isolate_varint(bytes, index)
		var result : PBTypeTag = PBTypeTag.new()
		if varint_bytes.size() != 0:
			result.ok = true
			result.offset = varint_bytes.size()
			var unpacked : int = unpack_varint(varint_bytes)
			result.type = unpacked & 0x07
			result.tag = unpacked >> 3
		return result

	static func pack_length_delimeted(type : int, tag : int, bytes : PackedByteArray) -> PackedByteArray:
		var result : PackedByteArray = pack_type_tag(type, tag)
		result.append_array(pack_varint(bytes.size()))
		result.append_array(bytes)
		return result

	static func pb_type_from_data_type(data_type : int) -> int:
		if data_type == PB_DATA_TYPE.INT32 || data_type == PB_DATA_TYPE.SINT32 || data_type == PB_DATA_TYPE.UINT32 || data_type == PB_DATA_TYPE.INT64 || data_type == PB_DATA_TYPE.SINT64 || data_type == PB_DATA_TYPE.UINT64 || data_type == PB_DATA_TYPE.BOOL || data_type == PB_DATA_TYPE.ENUM:
			return PB_TYPE.VARINT
		elif data_type == PB_DATA_TYPE.FIXED32 || data_type == PB_DATA_TYPE.SFIXED32 || data_type == PB_DATA_TYPE.FLOAT:
			return PB_TYPE.FIX32
		elif data_type == PB_DATA_TYPE.FIXED64 || data_type == PB_DATA_TYPE.SFIXED64 || data_type == PB_DATA_TYPE.DOUBLE:
			return PB_TYPE.FIX64
		elif data_type == PB_DATA_TYPE.STRING || data_type == PB_DATA_TYPE.BYTES || data_type == PB_DATA_TYPE.MESSAGE || data_type == PB_DATA_TYPE.MAP:
			return PB_TYPE.LENGTHDEL
		else:
			return PB_TYPE.UNDEFINED

	static func pack_field(field : PBField) -> PackedByteArray:
		var type : int = pb_type_from_data_type(field.type)
		var type_copy : int = type
		if field.rule == PB_RULE.REPEATED && field.option_packed:
			type = PB_TYPE.LENGTHDEL
		var head : PackedByteArray = pack_type_tag(type, field.tag)
		var data : PackedByteArray = PackedByteArray()
		if type == PB_TYPE.VARINT:
			var value
			if field.rule == PB_RULE.REPEATED:
				for v in field.value:
					data.append_array(head)
					if field.type == PB_DATA_TYPE.SINT32 || field.type == PB_DATA_TYPE.SINT64:
						value = convert_signed(v)
					else:
						value = v
					data.append_array(pack_varint(value))
				return data
			else:
				if field.type == PB_DATA_TYPE.SINT32 || field.type == PB_DATA_TYPE.SINT64:
					value = convert_signed(field.value)
				else:
					value = field.value
				data = pack_varint(value)
		elif type == PB_TYPE.FIX32:
			if field.rule == PB_RULE.REPEATED:
				for v in field.value:
					data.append_array(head)
					data.append_array(pack_bytes(v, 4, field.type))
				return data
			else:
				data.append_array(pack_bytes(field.value, 4, field.type))
		elif type == PB_TYPE.FIX64:
			if field.rule == PB_RULE.REPEATED:
				for v in field.value:
					data.append_array(head)
					data.append_array(pack_bytes(v, 8, field.type))
				return data
			else:
				data.append_array(pack_bytes(field.value, 8, field.type))
		elif type == PB_TYPE.LENGTHDEL:
			if field.rule == PB_RULE.REPEATED:
				if type_copy == PB_TYPE.VARINT:
					if field.type == PB_DATA_TYPE.SINT32 || field.type == PB_DATA_TYPE.SINT64:
						var signed_value : int
						for v in field.value:
							signed_value = convert_signed(v)
							data.append_array(pack_varint(signed_value))
					else:
						for v in field.value:
							data.append_array(pack_varint(v))
					return pack_length_delimeted(type, field.tag, data)
				elif type_copy == PB_TYPE.FIX32:
					for v in field.value:
						data.append_array(pack_bytes(v, 4, field.type))
					return pack_length_delimeted(type, field.tag, data)
				elif type_copy == PB_TYPE.FIX64:
					for v in field.value:
						data.append_array(pack_bytes(v, 8, field.type))
					return pack_length_delimeted(type, field.tag, data)
				elif field.type == PB_DATA_TYPE.STRING:
					for v in field.value:
						var obj = v.to_utf8_buffer()
						data.append_array(pack_length_delimeted(type, field.tag, obj))
					return data
				elif field.type == PB_DATA_TYPE.BYTES:
					for v in field.value:
						data.append_array(pack_length_delimeted(type, field.tag, v))
					return data
				elif typeof(field.value[0]) == TYPE_OBJECT:
					for v in field.value:
						var obj : PackedByteArray = v.to_bytes()
						data.append_array(pack_length_delimeted(type, field.tag, obj))
					return data
			else:
				if field.type == PB_DATA_TYPE.STRING:
					var str_bytes : PackedByteArray = field.value.to_utf8_buffer()
					if PROTO_VERSION == 2 || (PROTO_VERSION == 3 && str_bytes.size() > 0):
						data.append_array(str_bytes)
						return pack_length_delimeted(type, field.tag, data)
				if field.type == PB_DATA_TYPE.BYTES:
					if PROTO_VERSION == 2 || (PROTO_VERSION == 3 && field.value.size() > 0):
						data.append_array(field.value)
						return pack_length_delimeted(type, field.tag, data)
				elif typeof(field.value) == TYPE_OBJECT:
					var obj : PackedByteArray = field.value.to_bytes()
					if obj.size() > 0:
						data.append_array(obj)
					return pack_length_delimeted(type, field.tag, data)
				else:
					pass
		if data.size() > 0:
			head.append_array(data)
			return head
		else:
			return data

	static func skip_unknown_field(bytes : PackedByteArray, offset : int, type : int) -> int:
		if type == PB_TYPE.VARINT:
			return offset + isolate_varint(bytes, offset).size()
		if type == PB_TYPE.FIX64:
			return offset + 8
		if type == PB_TYPE.LENGTHDEL:
			var length_bytes : PackedByteArray = isolate_varint(bytes, offset)
			var length : int = unpack_varint(length_bytes)
			return offset + length_bytes.size() + length
		if type == PB_TYPE.FIX32:
			return offset + 4
		return PB_ERR.UNDEFINED_STATE

	static func unpack_field(bytes : PackedByteArray, offset : int, field : PBField, type : int, message_func_ref) -> int:
		if field.rule == PB_RULE.REPEATED && type != PB_TYPE.LENGTHDEL && field.option_packed:
			var count = isolate_varint(bytes, offset)
			if count.size() > 0:
				offset += count.size()
				count = unpack_varint(count)
				if type == PB_TYPE.VARINT:
					var val
					var counter = offset + count
					while offset < counter:
						val = isolate_varint(bytes, offset)
						if val.size() > 0:
							offset += val.size()
							val = unpack_varint(val)
							if field.type == PB_DATA_TYPE.SINT32 || field.type == PB_DATA_TYPE.SINT64:
								val = deconvert_signed(val)
							elif field.type == PB_DATA_TYPE.BOOL:
								if val:
									val = true
								else:
									val = false
							field.value.append(val)
						else:
							return PB_ERR.REPEATED_COUNT_MISMATCH
					return offset
				elif type == PB_TYPE.FIX32 || type == PB_TYPE.FIX64:
					var type_size
					if type == PB_TYPE.FIX32:
						type_size = 4
					else:
						type_size = 8
					var val
					var counter = offset + count
					while offset < counter:
						if (offset + type_size) > bytes.size():
							return PB_ERR.REPEATED_COUNT_MISMATCH
						val = unpack_bytes(bytes, offset, type_size, field.type)
						offset += type_size
						field.value.append(val)
					return offset
			else:
				return PB_ERR.REPEATED_COUNT_NOT_FOUND
		else:
			if type == PB_TYPE.VARINT:
				var val = isolate_varint(bytes, offset)
				if val.size() > 0:
					offset += val.size()
					val = unpack_varint(val)
					if field.type == PB_DATA_TYPE.SINT32 || field.type == PB_DATA_TYPE.SINT64:
						val = deconvert_signed(val)
					elif field.type == PB_DATA_TYPE.BOOL:
						if val:
							val = true
						else:
							val = false
					if field.rule == PB_RULE.REPEATED:
						field.value.append(val)
					else:
						field.value = val
				else:
					return PB_ERR.VARINT_NOT_FOUND
				return offset
			elif type == PB_TYPE.FIX32 || type == PB_TYPE.FIX64:
				var type_size
				if type == PB_TYPE.FIX32:
					type_size = 4
				else:
					type_size = 8
				var val
				if (offset + type_size) > bytes.size():
					return PB_ERR.REPEATED_COUNT_MISMATCH
				val = unpack_bytes(bytes, offset, type_size, field.type)
				offset += type_size
				if field.rule == PB_RULE.REPEATED:
					field.value.append(val)
				else:
					field.value = val
				return offset
			elif type == PB_TYPE.LENGTHDEL:
				var inner_size = isolate_varint(bytes, offset)
				if inner_size.size() > 0:
					offset += inner_size.size()
					inner_size = unpack_varint(inner_size)
					if inner_size >= 0:
						if inner_size + offset > bytes.size():
							return PB_ERR.LENGTHDEL_SIZE_MISMATCH
						if message_func_ref != null:
							var message = message_func_ref.call()
							if inner_size > 0:
								var sub_offset = message.from_bytes(bytes, offset, inner_size + offset)
								if sub_offset > 0:
									if sub_offset - offset >= inner_size:
										offset = sub_offset
										return offset
									else:
										return PB_ERR.LENGTHDEL_SIZE_MISMATCH
								return sub_offset
							else:
								return offset
						elif field.type == PB_DATA_TYPE.STRING:
							var str_bytes : PackedByteArray = bytes.slice(offset, inner_size + offset)
							if field.rule == PB_RULE.REPEATED:
								field.value.append(str_bytes.get_string_from_utf8())
							else:
								field.value = str_bytes.get_string_from_utf8()
							return offset + inner_size
						elif field.type == PB_DATA_TYPE.BYTES:
							var val_bytes : PackedByteArray = bytes.slice(offset, inner_size + offset)
							if field.rule == PB_RULE.REPEATED:
								field.value.append(val_bytes)
							else:
								field.value = val_bytes
							return offset + inner_size
					else:
						return PB_ERR.LENGTHDEL_SIZE_NOT_FOUND
				else:
					return PB_ERR.LENGTHDEL_SIZE_NOT_FOUND
		return PB_ERR.UNDEFINED_STATE

	static func unpack_message(data, bytes : PackedByteArray, offset : int, limit : int) -> int:
		while true:
			var tt : PBTypeTag = unpack_type_tag(bytes, offset)
			if tt.ok:
				offset += tt.offset
				if data.has(tt.tag):
					var service : PBServiceField = data[tt.tag]
					var type : int = pb_type_from_data_type(service.field.type)
					if type == tt.type || (tt.type == PB_TYPE.LENGTHDEL && service.field.rule == PB_RULE.REPEATED && service.field.option_packed):
						var res : int = unpack_field(bytes, offset, service.field, type, service.func_ref)
						if res > 0:
							service.state = PB_SERVICE_STATE.FILLED
							offset = res
							if offset == limit:
								return offset
							elif offset > limit:
								return PB_ERR.PACKAGE_SIZE_MISMATCH
						elif res < 0:
							return res
						else:
							break
				else:
					var res : int = skip_unknown_field(bytes, offset, tt.type)
					if res > 0:
						offset = res
						if offset == limit:
							return offset
						elif offset > limit:
							return PB_ERR.PACKAGE_SIZE_MISMATCH
					elif res < 0:
						return res
					else:
						break							
			else:
				return offset
		return PB_ERR.UNDEFINED_STATE

	static func pack_message(data) -> PackedByteArray:
		var DEFAULT_VALUES
		if PROTO_VERSION == 2:
			DEFAULT_VALUES = DEFAULT_VALUES_2
		elif PROTO_VERSION == 3:
			DEFAULT_VALUES = DEFAULT_VALUES_3
		var result : PackedByteArray = PackedByteArray()
		var keys : Array = data.keys()
		keys.sort()
		for i in keys:
			if data[i].field.value != null:
				if data[i].state == PB_SERVICE_STATE.UNFILLED \
				&& !data[i].field.is_map_field \
				&& typeof(data[i].field.value) == typeof(DEFAULT_VALUES[data[i].field.type]) \
				&& data[i].field.value == DEFAULT_VALUES[data[i].field.type]:
					continue
				elif data[i].field.rule == PB_RULE.REPEATED && data[i].field.value.size() == 0:
					continue
				result.append_array(pack_field(data[i].field))
			elif data[i].field.rule == PB_RULE.REQUIRED:
				print("Error: required field is not filled: Tag:", data[i].field.tag)
				return PackedByteArray()
		return result

	static func check_required(data) -> bool:
		var keys : Array = data.keys()
		for i in keys:
			if data[i].field.rule == PB_RULE.REQUIRED && data[i].state == PB_SERVICE_STATE.UNFILLED:
				return false
		return true

	static func construct_map(key_values):
		var result = {}
		for kv in key_values:
			result[kv.get_key()] = kv.get_value()
		return result
	
	static func tabulate(text : String, nesting : int) -> String:
		var tab : String = ""
		for _i in range(nesting):
			tab += DEBUG_TAB
		return tab + text
	
	static func value_to_string(value, field : PBField, nesting : int) -> String:
		var result : String = ""
		var text : String
		if field.type == PB_DATA_TYPE.MESSAGE:
			result += "{"
			nesting += 1
			text = message_to_string(value.data, nesting)
			if text != "":
				result += "\n" + text
				nesting -= 1
				result += tabulate("}", nesting)
			else:
				nesting -= 1
				result += "}"
		elif field.type == PB_DATA_TYPE.BYTES:
			result += "<"
			for i in range(value.size()):
				result += str(value[i])
				if i != (value.size() - 1):
					result += ", "
			result += ">"
		elif field.type == PB_DATA_TYPE.STRING:
			result += "\"" + value + "\""
		elif field.type == PB_DATA_TYPE.ENUM:
			result += "ENUM::" + str(value)
		else:
			result += str(value)
		return result
	
	static func field_to_string(field : PBField, nesting : int) -> String:
		var result : String = tabulate(field.name + ": ", nesting)
		if field.type == PB_DATA_TYPE.MAP:
			if field.value.size() > 0:
				result += "(\n"
				nesting += 1
				for i in range(field.value.size()):
					var local_key_value = field.value[i].data[1].field
					result += tabulate(value_to_string(local_key_value.value, local_key_value, nesting), nesting) + ": "
					local_key_value = field.value[i].data[2].field
					result += value_to_string(local_key_value.value, local_key_value, nesting)
					if i != (field.value.size() - 1):
						result += ","
					result += "\n"
				nesting -= 1
				result += tabulate(")", nesting)
			else:
				result += "()"
		elif field.rule == PB_RULE.REPEATED:
			if field.value.size() > 0:
				result += "[\n"
				nesting += 1
				for i in range(field.value.size()):
					result += tabulate(str(i) + ": ", nesting)
					result += value_to_string(field.value[i], field, nesting)
					if i != (field.value.size() - 1):
						result += ","
					result += "\n"
				nesting -= 1
				result += tabulate("]", nesting)
			else:
				result += "[]"
		else:
			result += value_to_string(field.value, field, nesting)
		result += ";\n"
		return result
		
	static func message_to_string(data, nesting : int = 0) -> String:
		var DEFAULT_VALUES
		if PROTO_VERSION == 2:
			DEFAULT_VALUES = DEFAULT_VALUES_2
		elif PROTO_VERSION == 3:
			DEFAULT_VALUES = DEFAULT_VALUES_3
		var result : String = ""
		var keys : Array = data.keys()
		keys.sort()
		for i in keys:
			if data[i].field.value != null:
				if data[i].state == PB_SERVICE_STATE.UNFILLED \
				&& !data[i].field.is_map_field \
				&& typeof(data[i].field.value) == typeof(DEFAULT_VALUES[data[i].field.type]) \
				&& data[i].field.value == DEFAULT_VALUES[data[i].field.type]:
					continue
				elif data[i].field.rule == PB_RULE.REPEATED && data[i].field.value.size() == 0:
					continue
				result += field_to_string(data[i].field, nesting)
			elif data[i].field.rule == PB_RULE.REQUIRED:
				result += data[i].field.name + ": " + "error"
		return result



############### USER DATA BEGIN ################


class ClientMessage:
	func _init():
		var service
		
		__protocol_version = PBField.new("protocol_version", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __protocol_version
		data[__protocol_version.tag] = service
		
		__join = PBField.new("join", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 10, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __join
		service.func_ref = Callable(self, "new_join")
		data[__join.tag] = service
		
		__move = PBField.new("move", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 11, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __move
		service.func_ref = Callable(self, "new_move")
		data[__move.tag] = service
		
		__attack = PBField.new("attack", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 12, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __attack
		service.func_ref = Callable(self, "new_attack")
		data[__attack.tag] = service
		
		__pickup = PBField.new("pickup", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 13, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __pickup
		service.func_ref = Callable(self, "new_pickup")
		data[__pickup.tag] = service
		
		__equip = PBField.new("equip", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 14, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __equip
		service.func_ref = Callable(self, "new_equip")
		data[__equip.tag] = service
		
		__unequip = PBField.new("unequip", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 15, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __unequip
		service.func_ref = Callable(self, "new_unequip")
		data[__unequip.tag] = service
		
		__drop = PBField.new("drop", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 16, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __drop
		service.func_ref = Callable(self, "new_drop")
		data[__drop.tag] = service
		
		__use = PBField.new("use", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 17, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __use
		service.func_ref = Callable(self, "new_use")
		data[__use.tag] = service
		
		__loot_open = PBField.new("loot_open", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 18, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __loot_open
		service.func_ref = Callable(self, "new_loot_open")
		data[__loot_open.tag] = service
		
		__loot_close = PBField.new("loot_close", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 19, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __loot_close
		service.func_ref = Callable(self, "new_loot_close")
		data[__loot_close.tag] = service
		
		__stash_move = PBField.new("stash_move", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 21, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __stash_move
		service.func_ref = Callable(self, "new_stash_move")
		data[__stash_move.tag] = service
		
		__instance_join = PBField.new("instance_join", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 22, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __instance_join
		service.func_ref = Callable(self, "new_instance_join")
		data[__instance_join.tag] = service
		
		__chat = PBField.new("chat", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 23, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __chat
		service.func_ref = Callable(self, "new_chat")
		data[__chat.tag] = service
		
	var data = {}
	
	enum PayloadCase {
		PAYLOAD_NOT_SET = 0,
		JOIN = 10,
		MOVE = 11,
		ATTACK = 12,
		PICKUP = 13,
		EQUIP = 14,
		UNEQUIP = 15,
		DROP = 16,
		USE = 17,
		LOOT_OPEN = 18,
		LOOT_CLOSE = 19,
		STASH_MOVE = 21,
		INSTANCE_JOIN = 22,
		CHAT = 23,
	}
	var _payload_case: int = 0

	var __protocol_version: PBField
	func has_protocol_version() -> bool:
		if __protocol_version.value != null:
			return true
		return false
	func get_protocol_version() -> int:
		return __protocol_version.value
	func clear_protocol_version() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__protocol_version.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_protocol_version(value : int) -> void:
		__protocol_version.value = value
	
	var __join: PBField
	func has_join() -> bool:
		return data[10].state == PB_SERVICE_STATE.FILLED
	func get_join() -> JoinRequest:
		return __join.value
	func clear_join() -> void:
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_join() -> JoinRequest:
		data[10].state = PB_SERVICE_STATE.FILLED
		_payload_case = 10
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__join.value = JoinRequest.new()
		return __join.value
	
	var __move: PBField
	func has_move() -> bool:
		return data[11].state == PB_SERVICE_STATE.FILLED
	func get_move() -> MoveInput:
		return __move.value
	func clear_move() -> void:
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_move() -> MoveInput:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		data[11].state = PB_SERVICE_STATE.FILLED
		_payload_case = 11
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__move.value = MoveInput.new()
		return __move.value
	
	var __attack: PBField
	func has_attack() -> bool:
		return data[12].state == PB_SERVICE_STATE.FILLED
	func get_attack() -> AttackRequest:
		return __attack.value
	func clear_attack() -> void:
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_attack() -> AttackRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		data[12].state = PB_SERVICE_STATE.FILLED
		_payload_case = 12
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = AttackRequest.new()
		return __attack.value
	
	var __pickup: PBField
	func has_pickup() -> bool:
		return data[13].state == PB_SERVICE_STATE.FILLED
	func get_pickup() -> PickupRequest:
		return __pickup.value
	func clear_pickup() -> void:
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_pickup() -> PickupRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		data[13].state = PB_SERVICE_STATE.FILLED
		_payload_case = 13
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = PickupRequest.new()
		return __pickup.value
	
	var __equip: PBField
	func has_equip() -> bool:
		return data[14].state == PB_SERVICE_STATE.FILLED
	func get_equip() -> EquipRequest:
		return __equip.value
	func clear_equip() -> void:
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_equip() -> EquipRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		data[14].state = PB_SERVICE_STATE.FILLED
		_payload_case = 14
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = EquipRequest.new()
		return __equip.value
	
	var __unequip: PBField
	func has_unequip() -> bool:
		return data[15].state == PB_SERVICE_STATE.FILLED
	func get_unequip() -> UnequipRequest:
		return __unequip.value
	func clear_unequip() -> void:
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_unequip() -> UnequipRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		data[15].state = PB_SERVICE_STATE.FILLED
		_payload_case = 15
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = UnequipRequest.new()
		return __unequip.value
	
	var __drop: PBField
	func has_drop() -> bool:
		return data[16].state == PB_SERVICE_STATE.FILLED
	func get_drop() -> DropRequest:
		return __drop.value
	func clear_drop() -> void:
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_drop() -> DropRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		data[16].state = PB_SERVICE_STATE.FILLED
		_payload_case = 16
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DropRequest.new()
		return __drop.value
	
	var __use: PBField
	func has_use() -> bool:
		return data[17].state == PB_SERVICE_STATE.FILLED
	func get_use() -> UseRequest:
		return __use.value
	func clear_use() -> void:
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_use() -> UseRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		data[17].state = PB_SERVICE_STATE.FILLED
		_payload_case = 17
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__use.value = UseRequest.new()
		return __use.value
	
	var __loot_open: PBField
	func has_loot_open() -> bool:
		return data[18].state == PB_SERVICE_STATE.FILLED
	func get_loot_open() -> LootOpenRequest:
		return __loot_open.value
	func clear_loot_open() -> void:
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_loot_open() -> LootOpenRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		data[18].state = PB_SERVICE_STATE.FILLED
		_payload_case = 18
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = LootOpenRequest.new()
		return __loot_open.value
	
	var __loot_close: PBField
	func has_loot_close() -> bool:
		return data[19].state == PB_SERVICE_STATE.FILLED
	func get_loot_close() -> LootCloseRequest:
		return __loot_close.value
	func clear_loot_close() -> void:
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_loot_close() -> LootCloseRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		data[19].state = PB_SERVICE_STATE.FILLED
		_payload_case = 19
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = LootCloseRequest.new()
		return __loot_close.value
	
	var __stash_move: PBField
	func has_stash_move() -> bool:
		return data[21].state == PB_SERVICE_STATE.FILLED
	func get_stash_move() -> StashMoveRequest:
		return __stash_move.value
	func clear_stash_move() -> void:
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_stash_move() -> StashMoveRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		data[21].state = PB_SERVICE_STATE.FILLED
		_payload_case = 21
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = StashMoveRequest.new()
		return __stash_move.value
	
	var __instance_join: PBField
	func has_instance_join() -> bool:
		return data[22].state == PB_SERVICE_STATE.FILLED
	func get_instance_join() -> InstanceJoinRequest:
		return __instance_join.value
	func clear_instance_join() -> void:
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_instance_join() -> InstanceJoinRequest:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		data[22].state = PB_SERVICE_STATE.FILLED
		_payload_case = 22
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = InstanceJoinRequest.new()
		return __instance_join.value
	
	var __chat: PBField
	func has_chat() -> bool:
		return data[23].state == PB_SERVICE_STATE.FILLED
	func get_chat() -> ChatSend:
		return __chat.value
	func clear_chat() -> void:
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_chat() -> ChatSend:
		__join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__attack.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__pickup.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__equip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__unequip.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__drop.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__use.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot_open.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot_close.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__stash_move.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__instance_join.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		data[23].state = PB_SERVICE_STATE.FILLED
		_payload_case = 23
		__chat.value = ChatSend.new()
		return __chat.value
	
	func get_payload_case() -> int:
		return _payload_case
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class ServerMessage:
	func _init():
		var service
		
		__protocol_version = PBField.new("protocol_version", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __protocol_version
		data[__protocol_version.tag] = service
		
		__joined = PBField.new("joined", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 10, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __joined
		service.func_ref = Callable(self, "new_joined")
		data[__joined.tag] = service
		
		__snapshot = PBField.new("snapshot", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 11, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __snapshot
		service.func_ref = Callable(self, "new_snapshot")
		data[__snapshot.tag] = service
		
		__damage = PBField.new("damage", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 12, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __damage
		service.func_ref = Callable(self, "new_damage")
		data[__damage.tag] = service
		
		__inventory = PBField.new("inventory", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 13, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __inventory
		service.func_ref = Callable(self, "new_inventory")
		data[__inventory.tag] = service
		
		__extracted = PBField.new("extracted", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 14, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __extracted
		service.func_ref = Callable(self, "new_extracted")
		data[__extracted.tag] = service
		
		__equipment = PBField.new("equipment", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 15, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __equipment
		service.func_ref = Callable(self, "new_equipment")
		data[__equipment.tag] = service
		
		__ability_used = PBField.new("ability_used", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 16, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __ability_used
		service.func_ref = Callable(self, "new_ability_used")
		data[__ability_used.tag] = service
		
		__ability_rejected = PBField.new("ability_rejected", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 17, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __ability_rejected
		service.func_ref = Callable(self, "new_ability_rejected")
		data[__ability_rejected.tag] = service
		
		__loot = PBField.new("loot", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 18, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __loot
		service.func_ref = Callable(self, "new_loot")
		data[__loot.tag] = service
		
		__pickup_rejected = PBField.new("pickup_rejected", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 19, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __pickup_rejected
		service.func_ref = Callable(self, "new_pickup_rejected")
		data[__pickup_rejected.tag] = service
		
		__xp_gained = PBField.new("xp_gained", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 20, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __xp_gained
		service.func_ref = Callable(self, "new_xp_gained")
		data[__xp_gained.tag] = service
		
		__level_up = PBField.new("level_up", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 21, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __level_up
		service.func_ref = Callable(self, "new_level_up")
		data[__level_up.tag] = service
		
		__run_ended = PBField.new("run_ended", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 22, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __run_ended
		service.func_ref = Callable(self, "new_run_ended")
		data[__run_ended.tag] = service
		
		__lobby = PBField.new("lobby", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 23, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __lobby
		service.func_ref = Callable(self, "new_lobby")
		data[__lobby.tag] = service
		
		__stash = PBField.new("stash", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 24, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __stash
		service.func_ref = Callable(self, "new_stash")
		data[__stash.tag] = service
		
		__stash_move_rejected = PBField.new("stash_move_rejected", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 25, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __stash_move_rejected
		service.func_ref = Callable(self, "new_stash_move_rejected")
		data[__stash_move_rejected.tag] = service
		
		__enter_instance_rejected = PBField.new("enter_instance_rejected", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 26, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __enter_instance_rejected
		service.func_ref = Callable(self, "new_enter_instance_rejected")
		data[__enter_instance_rejected.tag] = service
		
		__ticket = PBField.new("ticket", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 27, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __ticket
		service.func_ref = Callable(self, "new_ticket")
		data[__ticket.tag] = service
		
		__town_ticket = PBField.new("town_ticket", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 28, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __town_ticket
		service.func_ref = Callable(self, "new_town_ticket")
		data[__town_ticket.tag] = service
		
		__chat = PBField.new("chat", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 29, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __chat
		service.func_ref = Callable(self, "new_chat")
		data[__chat.tag] = service
		
		__chat_channels = PBField.new("chat_channels", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 30, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __chat_channels
		service.func_ref = Callable(self, "new_chat_channels")
		data[__chat_channels.tag] = service
		
	var data = {}
	
	enum PayloadCase {
		PAYLOAD_NOT_SET = 0,
		JOINED = 10,
		SNAPSHOT = 11,
		DAMAGE = 12,
		INVENTORY = 13,
		EXTRACTED = 14,
		EQUIPMENT = 15,
		ABILITY_USED = 16,
		ABILITY_REJECTED = 17,
		LOOT = 18,
		PICKUP_REJECTED = 19,
		XP_GAINED = 20,
		LEVEL_UP = 21,
		RUN_ENDED = 22,
		LOBBY = 23,
		STASH = 24,
		STASH_MOVE_REJECTED = 25,
		ENTER_INSTANCE_REJECTED = 26,
		TICKET = 27,
		TOWN_TICKET = 28,
		CHAT = 29,
		CHAT_CHANNELS = 30,
	}
	var _payload_case: int = 0

	var __protocol_version: PBField
	func has_protocol_version() -> bool:
		if __protocol_version.value != null:
			return true
		return false
	func get_protocol_version() -> int:
		return __protocol_version.value
	func clear_protocol_version() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__protocol_version.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_protocol_version(value : int) -> void:
		__protocol_version.value = value
	
	var __joined: PBField
	func has_joined() -> bool:
		return data[10].state == PB_SERVICE_STATE.FILLED
	func get_joined() -> JoinAccepted:
		return __joined.value
	func clear_joined() -> void:
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_joined() -> JoinAccepted:
		data[10].state = PB_SERVICE_STATE.FILLED
		_payload_case = 10
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__joined.value = JoinAccepted.new()
		return __joined.value
	
	var __snapshot: PBField
	func has_snapshot() -> bool:
		return data[11].state == PB_SERVICE_STATE.FILLED
	func get_snapshot() -> WorldSnapshot:
		return __snapshot.value
	func clear_snapshot() -> void:
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_snapshot() -> WorldSnapshot:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		data[11].state = PB_SERVICE_STATE.FILLED
		_payload_case = 11
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = WorldSnapshot.new()
		return __snapshot.value
	
	var __damage: PBField
	func has_damage() -> bool:
		return data[12].state == PB_SERVICE_STATE.FILLED
	func get_damage() -> DamageEvent:
		return __damage.value
	func clear_damage() -> void:
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_damage() -> DamageEvent:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		data[12].state = PB_SERVICE_STATE.FILLED
		_payload_case = 12
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DamageEvent.new()
		return __damage.value
	
	var __inventory: PBField
	func has_inventory() -> bool:
		return data[13].state == PB_SERVICE_STATE.FILLED
	func get_inventory() -> InventoryUpdated:
		return __inventory.value
	func clear_inventory() -> void:
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_inventory() -> InventoryUpdated:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		data[13].state = PB_SERVICE_STATE.FILLED
		_payload_case = 13
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = InventoryUpdated.new()
		return __inventory.value
	
	var __extracted: PBField
	func has_extracted() -> bool:
		return data[14].state == PB_SERVICE_STATE.FILLED
	func get_extracted() -> Extracted:
		return __extracted.value
	func clear_extracted() -> void:
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_extracted() -> Extracted:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		data[14].state = PB_SERVICE_STATE.FILLED
		_payload_case = 14
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = Extracted.new()
		return __extracted.value
	
	var __equipment: PBField
	func has_equipment() -> bool:
		return data[15].state == PB_SERVICE_STATE.FILLED
	func get_equipment() -> EquipmentUpdated:
		return __equipment.value
	func clear_equipment() -> void:
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_equipment() -> EquipmentUpdated:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		data[15].state = PB_SERVICE_STATE.FILLED
		_payload_case = 15
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = EquipmentUpdated.new()
		return __equipment.value
	
	var __ability_used: PBField
	func has_ability_used() -> bool:
		return data[16].state == PB_SERVICE_STATE.FILLED
	func get_ability_used() -> AbilityUsed:
		return __ability_used.value
	func clear_ability_used() -> void:
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_ability_used() -> AbilityUsed:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		data[16].state = PB_SERVICE_STATE.FILLED
		_payload_case = 16
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = AbilityUsed.new()
		return __ability_used.value
	
	var __ability_rejected: PBField
	func has_ability_rejected() -> bool:
		return data[17].state == PB_SERVICE_STATE.FILLED
	func get_ability_rejected() -> AbilityRejected:
		return __ability_rejected.value
	func clear_ability_rejected() -> void:
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_ability_rejected() -> AbilityRejected:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		data[17].state = PB_SERVICE_STATE.FILLED
		_payload_case = 17
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = AbilityRejected.new()
		return __ability_rejected.value
	
	var __loot: PBField
	func has_loot() -> bool:
		return data[18].state == PB_SERVICE_STATE.FILLED
	func get_loot() -> LootContents:
		return __loot.value
	func clear_loot() -> void:
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_loot() -> LootContents:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		data[18].state = PB_SERVICE_STATE.FILLED
		_payload_case = 18
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = LootContents.new()
		return __loot.value
	
	var __pickup_rejected: PBField
	func has_pickup_rejected() -> bool:
		return data[19].state == PB_SERVICE_STATE.FILLED
	func get_pickup_rejected() -> PickupRejected:
		return __pickup_rejected.value
	func clear_pickup_rejected() -> void:
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_pickup_rejected() -> PickupRejected:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		data[19].state = PB_SERVICE_STATE.FILLED
		_payload_case = 19
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = PickupRejected.new()
		return __pickup_rejected.value
	
	var __xp_gained: PBField
	func has_xp_gained() -> bool:
		return data[20].state == PB_SERVICE_STATE.FILLED
	func get_xp_gained() -> XpGained:
		return __xp_gained.value
	func clear_xp_gained() -> void:
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_xp_gained() -> XpGained:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		data[20].state = PB_SERVICE_STATE.FILLED
		_payload_case = 20
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = XpGained.new()
		return __xp_gained.value
	
	var __level_up: PBField
	func has_level_up() -> bool:
		return data[21].state == PB_SERVICE_STATE.FILLED
	func get_level_up() -> LevelUp:
		return __level_up.value
	func clear_level_up() -> void:
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_level_up() -> LevelUp:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		data[21].state = PB_SERVICE_STATE.FILLED
		_payload_case = 21
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = LevelUp.new()
		return __level_up.value
	
	var __run_ended: PBField
	func has_run_ended() -> bool:
		return data[22].state == PB_SERVICE_STATE.FILLED
	func get_run_ended() -> RunEnded:
		return __run_ended.value
	func clear_run_ended() -> void:
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_run_ended() -> RunEnded:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		data[22].state = PB_SERVICE_STATE.FILLED
		_payload_case = 22
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = RunEnded.new()
		return __run_ended.value
	
	var __lobby: PBField
	func has_lobby() -> bool:
		return data[23].state == PB_SERVICE_STATE.FILLED
	func get_lobby() -> Lobby:
		return __lobby.value
	func clear_lobby() -> void:
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_lobby() -> Lobby:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		data[23].state = PB_SERVICE_STATE.FILLED
		_payload_case = 23
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = Lobby.new()
		return __lobby.value
	
	var __stash: PBField
	func has_stash() -> bool:
		return data[24].state == PB_SERVICE_STATE.FILLED
	func get_stash() -> StashContents:
		return __stash.value
	func clear_stash() -> void:
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_stash() -> StashContents:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		data[24].state = PB_SERVICE_STATE.FILLED
		_payload_case = 24
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = StashContents.new()
		return __stash.value
	
	var __stash_move_rejected: PBField
	func has_stash_move_rejected() -> bool:
		return data[25].state == PB_SERVICE_STATE.FILLED
	func get_stash_move_rejected() -> StashMoveRejected:
		return __stash_move_rejected.value
	func clear_stash_move_rejected() -> void:
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_stash_move_rejected() -> StashMoveRejected:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		data[25].state = PB_SERVICE_STATE.FILLED
		_payload_case = 25
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = StashMoveRejected.new()
		return __stash_move_rejected.value
	
	var __enter_instance_rejected: PBField
	func has_enter_instance_rejected() -> bool:
		return data[26].state == PB_SERVICE_STATE.FILLED
	func get_enter_instance_rejected() -> EnterInstanceRejected:
		return __enter_instance_rejected.value
	func clear_enter_instance_rejected() -> void:
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_enter_instance_rejected() -> EnterInstanceRejected:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		data[26].state = PB_SERVICE_STATE.FILLED
		_payload_case = 26
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = EnterInstanceRejected.new()
		return __enter_instance_rejected.value
	
	var __ticket: PBField
	func has_ticket() -> bool:
		return data[27].state == PB_SERVICE_STATE.FILLED
	func get_ticket() -> InstanceTicket:
		return __ticket.value
	func clear_ticket() -> void:
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_ticket() -> InstanceTicket:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		data[27].state = PB_SERVICE_STATE.FILLED
		_payload_case = 27
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = InstanceTicket.new()
		return __ticket.value
	
	var __town_ticket: PBField
	func has_town_ticket() -> bool:
		return data[28].state == PB_SERVICE_STATE.FILLED
	func get_town_ticket() -> TownTicket:
		return __town_ticket.value
	func clear_town_ticket() -> void:
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_town_ticket() -> TownTicket:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		data[28].state = PB_SERVICE_STATE.FILLED
		_payload_case = 28
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = TownTicket.new()
		return __town_ticket.value
	
	var __chat: PBField
	func has_chat() -> bool:
		return data[29].state == PB_SERVICE_STATE.FILLED
	func get_chat() -> ChatMessage:
		return __chat.value
	func clear_chat() -> void:
		data[29].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_chat() -> ChatMessage:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		data[29].state = PB_SERVICE_STATE.FILLED
		_payload_case = 29
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = ChatMessage.new()
		return __chat.value
	
	var __chat_channels: PBField
	func has_chat_channels() -> bool:
		return data[30].state == PB_SERVICE_STATE.FILLED
	func get_chat_channels() -> ChatChannels:
		return __chat_channels.value
	func clear_chat_channels() -> void:
		data[30].state = PB_SERVICE_STATE.UNFILLED
		__chat_channels.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_chat_channels() -> ChatChannels:
		__joined.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__snapshot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__damage.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__inventory.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__equipment.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__ability_used.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__ability_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__loot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__pickup_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[19].state = PB_SERVICE_STATE.UNFILLED
		__xp_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[20].state = PB_SERVICE_STATE.UNFILLED
		__level_up.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[21].state = PB_SERVICE_STATE.UNFILLED
		__run_ended.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[22].state = PB_SERVICE_STATE.UNFILLED
		__lobby.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[23].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[24].state = PB_SERVICE_STATE.UNFILLED
		__stash_move_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[25].state = PB_SERVICE_STATE.UNFILLED
		__enter_instance_rejected.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[26].state = PB_SERVICE_STATE.UNFILLED
		__ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[27].state = PB_SERVICE_STATE.UNFILLED
		__town_ticket.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[28].state = PB_SERVICE_STATE.UNFILLED
		__chat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
		data[29].state = PB_SERVICE_STATE.UNFILLED
		data[30].state = PB_SERVICE_STATE.FILLED
		_payload_case = 30
		__chat_channels.value = ChatChannels.new()
		return __chat_channels.value
	
	func get_payload_case() -> int:
		return _payload_case
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class Vec2:
	func _init():
		var service
		
		__x = PBField.new("x", PB_DATA_TYPE.FLOAT, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT])
		service = PBServiceField.new()
		service.field = __x
		data[__x.tag] = service
		
		__y = PBField.new("y", PB_DATA_TYPE.FLOAT, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT])
		service = PBServiceField.new()
		service.field = __y
		data[__y.tag] = service
		
	var data = {}
	
	var __x: PBField
	func has_x() -> bool:
		if __x.value != null:
			return true
		return false
	func get_x() -> float:
		return __x.value
	func clear_x() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__x.value = DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT]
	func set_x(value : float) -> void:
		__x.value = value
	
	var __y: PBField
	func has_y() -> bool:
		if __y.value != null:
			return true
		return false
	func get_y() -> float:
		return __y.value
	func clear_y() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__y.value = DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT]
	func set_y(value : float) -> void:
		__y.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class Rect:
	func _init():
		var service
		
		__min = PBField.new("min", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __min
		service.func_ref = Callable(self, "new_min")
		data[__min.tag] = service
		
		__max = PBField.new("max", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __max
		service.func_ref = Callable(self, "new_max")
		data[__max.tag] = service
		
	var data = {}
	
	var __min: PBField
	func has_min() -> bool:
		if __min.value != null:
			return true
		return false
	func get_min() -> Vec2:
		return __min.value
	func clear_min() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__min.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_min() -> Vec2:
		__min.value = Vec2.new()
		return __min.value
	
	var __max: PBField
	func has_max() -> bool:
		if __max.value != null:
			return true
		return false
	func get_max() -> Vec2:
		return __max.value
	func clear_max() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__max.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_max() -> Vec2:
		__max.value = Vec2.new()
		return __max.value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class JoinRequest:
	func _init():
		var service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
		__class_id = PBField.new("class_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __class_id
		data[__class_id.tag] = service
		
		__returning = PBField.new("returning", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __returning
		data[__returning.tag] = service
		
		__bot = PBField.new("bot", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __bot
		data[__bot.tag] = service
		
		var __chat_left_default: Array[String] = []
		__chat_left = PBField.new("chat_left", PB_DATA_TYPE.STRING, PB_RULE.REPEATED, 5, true, __chat_left_default)
		service = PBServiceField.new()
		service.field = __chat_left
		data[__chat_left.tag] = service
		
	var data = {}
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	var __class_id: PBField
	func has_class_id() -> bool:
		if __class_id.value != null:
			return true
		return false
	func get_class_id() -> String:
		return __class_id.value
	func clear_class_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__class_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_class_id(value : String) -> void:
		__class_id.value = value
	
	var __returning: PBField
	func has_returning() -> bool:
		if __returning.value != null:
			return true
		return false
	func get_returning() -> bool:
		return __returning.value
	func clear_returning() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__returning.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_returning(value : bool) -> void:
		__returning.value = value
	
	var __bot: PBField
	func has_bot() -> bool:
		if __bot.value != null:
			return true
		return false
	func get_bot() -> bool:
		return __bot.value
	func clear_bot() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__bot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_bot(value : bool) -> void:
		__bot.value = value
	
	var __chat_left: PBField
	func get_chat_left() -> Array[String]:
		return __chat_left.value
	func clear_chat_left() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__chat_left.value.clear()
	func add_chat_left(value : String) -> void:
		__chat_left.value.append(value)
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class JoinAccepted:
	func _init():
		var service
		
		__player_id = PBField.new("player_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __player_id
		data[__player_id.tag] = service
		
		__layout = PBField.new("layout", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __layout
		service.func_ref = Callable(self, "new_layout")
		data[__layout.tag] = service
		
		__spawn = PBField.new("spawn", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __spawn
		service.func_ref = Callable(self, "new_spawn")
		data[__spawn.tag] = service
		
		__tick_rate = PBField.new("tick_rate", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __tick_rate
		data[__tick_rate.tag] = service
		
		__class_info = PBField.new("class_info", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __class_info
		service.func_ref = Callable(self, "new_class_info")
		data[__class_info.tag] = service
		
		__character = PBField.new("character", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 6, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __character
		service.func_ref = Callable(self, "new_character")
		data[__character.tag] = service
		
		__town = PBField.new("town", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 7, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __town
		data[__town.tag] = service
		
	var data = {}
	
	var __player_id: PBField
	func has_player_id() -> bool:
		if __player_id.value != null:
			return true
		return false
	func get_player_id() -> int:
		return __player_id.value
	func clear_player_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__player_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_player_id(value : int) -> void:
		__player_id.value = value
	
	var __layout: PBField
	func has_layout() -> bool:
		if __layout.value != null:
			return true
		return false
	func get_layout() -> InstanceLayout:
		return __layout.value
	func clear_layout() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__layout.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_layout() -> InstanceLayout:
		__layout.value = InstanceLayout.new()
		return __layout.value
	
	var __spawn: PBField
	func has_spawn() -> bool:
		if __spawn.value != null:
			return true
		return false
	func get_spawn() -> Vec2:
		return __spawn.value
	func clear_spawn() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__spawn.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_spawn() -> Vec2:
		__spawn.value = Vec2.new()
		return __spawn.value
	
	var __tick_rate: PBField
	func has_tick_rate() -> bool:
		if __tick_rate.value != null:
			return true
		return false
	func get_tick_rate() -> int:
		return __tick_rate.value
	func clear_tick_rate() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__tick_rate.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_tick_rate(value : int) -> void:
		__tick_rate.value = value
	
	var __class_info: PBField
	func has_class_info() -> bool:
		if __class_info.value != null:
			return true
		return false
	func get_class_info() -> ClassInfo:
		return __class_info.value
	func clear_class_info() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__class_info.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_class_info() -> ClassInfo:
		__class_info.value = ClassInfo.new()
		return __class_info.value
	
	var __character: PBField
	func has_character() -> bool:
		if __character.value != null:
			return true
		return false
	func get_character() -> CharacterInfo:
		return __character.value
	func clear_character() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__character.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_character() -> CharacterInfo:
		__character.value = CharacterInfo.new()
		return __character.value
	
	var __town: PBField
	func has_town() -> bool:
		if __town.value != null:
			return true
		return false
	func get_town() -> bool:
		return __town.value
	func clear_town() -> void:
		data[7].state = PB_SERVICE_STATE.UNFILLED
		__town.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_town(value : bool) -> void:
		__town.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class CharacterInfo:
	func _init():
		var service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
		__class_id = PBField.new("class_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __class_id
		data[__class_id.tag] = service
		
		__level = PBField.new("level", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __level
		data[__level.tag] = service
		
		__xp = PBField.new("xp", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __xp
		data[__xp.tag] = service
		
		__level_xp = PBField.new("level_xp", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __level_xp
		data[__level_xp.tag] = service
		
		__xp_to_next = PBField.new("xp_to_next", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 6, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __xp_to_next
		data[__xp_to_next.tag] = service
		
		__run_xp = PBField.new("run_xp", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 7, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __run_xp
		data[__run_xp.tag] = service
		
	var data = {}
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	var __class_id: PBField
	func has_class_id() -> bool:
		if __class_id.value != null:
			return true
		return false
	func get_class_id() -> String:
		return __class_id.value
	func clear_class_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__class_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_class_id(value : String) -> void:
		__class_id.value = value
	
	var __level: PBField
	func has_level() -> bool:
		if __level.value != null:
			return true
		return false
	func get_level() -> int:
		return __level.value
	func clear_level() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__level.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_level(value : int) -> void:
		__level.value = value
	
	var __xp: PBField
	func has_xp() -> bool:
		if __xp.value != null:
			return true
		return false
	func get_xp() -> int:
		return __xp.value
	func clear_xp() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__xp.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_xp(value : int) -> void:
		__xp.value = value
	
	var __level_xp: PBField
	func has_level_xp() -> bool:
		if __level_xp.value != null:
			return true
		return false
	func get_level_xp() -> int:
		return __level_xp.value
	func clear_level_xp() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__level_xp.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_level_xp(value : int) -> void:
		__level_xp.value = value
	
	var __xp_to_next: PBField
	func has_xp_to_next() -> bool:
		if __xp_to_next.value != null:
			return true
		return false
	func get_xp_to_next() -> int:
		return __xp_to_next.value
	func clear_xp_to_next() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__xp_to_next.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_xp_to_next(value : int) -> void:
		__xp_to_next.value = value
	
	var __run_xp: PBField
	func has_run_xp() -> bool:
		if __run_xp.value != null:
			return true
		return false
	func get_run_xp() -> int:
		return __run_xp.value
	func clear_run_xp() -> void:
		data[7].state = PB_SERVICE_STATE.UNFILLED
		__run_xp.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_run_xp(value : int) -> void:
		__run_xp.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class ClassInfo:
	func _init():
		var service
		
		__id = PBField.new("id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __id
		data[__id.tag] = service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
		__resource = PBField.new("resource", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __resource
		data[__resource.tag] = service
		
		__max_resource = PBField.new("max_resource", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __max_resource
		data[__max_resource.tag] = service
		
		__basic = PBField.new("basic", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __basic
		data[__basic.tag] = service
		
		var __abilities_default: Array[AbilityInfo] = []
		__abilities = PBField.new("abilities", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 6, true, __abilities_default)
		service = PBServiceField.new()
		service.field = __abilities
		service.func_ref = Callable(self, "add_abilities")
		data[__abilities.tag] = service
		
	var data = {}
	
	var __id: PBField
	func has_id() -> bool:
		if __id.value != null:
			return true
		return false
	func get_id() -> String:
		return __id.value
	func clear_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_id(value : String) -> void:
		__id.value = value
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	var __resource: PBField
	func has_resource() -> bool:
		if __resource.value != null:
			return true
		return false
	func get_resource() -> String:
		return __resource.value
	func clear_resource() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__resource.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_resource(value : String) -> void:
		__resource.value = value
	
	var __max_resource: PBField
	func has_max_resource() -> bool:
		if __max_resource.value != null:
			return true
		return false
	func get_max_resource() -> int:
		return __max_resource.value
	func clear_max_resource() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__max_resource.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_max_resource(value : int) -> void:
		__max_resource.value = value
	
	var __basic: PBField
	func has_basic() -> bool:
		if __basic.value != null:
			return true
		return false
	func get_basic() -> String:
		return __basic.value
	func clear_basic() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__basic.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_basic(value : String) -> void:
		__basic.value = value
	
	var __abilities: PBField
	func get_abilities() -> Array[AbilityInfo]:
		return __abilities.value
	func clear_abilities() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__abilities.value.clear()
	func add_abilities() -> AbilityInfo:
		var element = AbilityInfo.new()
		__abilities.value.append(element)
		return element
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class AbilityInfo:
	func _init():
		var service
		
		__id = PBField.new("id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __id
		data[__id.tag] = service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
		__cost = PBField.new("cost", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __cost
		data[__cost.tag] = service
		
		__cooldown_ms = PBField.new("cooldown_ms", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __cooldown_ms
		data[__cooldown_ms.tag] = service
		
		__windup_ms = PBField.new("windup_ms", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __windup_ms
		data[__windup_ms.tag] = service
		
		__root = PBField.new("root", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 6, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __root
		data[__root.tag] = service
		
		__unlock_level = PBField.new("unlock_level", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 7, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __unlock_level
		data[__unlock_level.tag] = service
		
	var data = {}
	
	var __id: PBField
	func has_id() -> bool:
		if __id.value != null:
			return true
		return false
	func get_id() -> String:
		return __id.value
	func clear_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_id(value : String) -> void:
		__id.value = value
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	var __cost: PBField
	func has_cost() -> bool:
		if __cost.value != null:
			return true
		return false
	func get_cost() -> int:
		return __cost.value
	func clear_cost() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__cost.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_cost(value : int) -> void:
		__cost.value = value
	
	var __cooldown_ms: PBField
	func has_cooldown_ms() -> bool:
		if __cooldown_ms.value != null:
			return true
		return false
	func get_cooldown_ms() -> int:
		return __cooldown_ms.value
	func clear_cooldown_ms() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__cooldown_ms.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_cooldown_ms(value : int) -> void:
		__cooldown_ms.value = value
	
	var __windup_ms: PBField
	func has_windup_ms() -> bool:
		if __windup_ms.value != null:
			return true
		return false
	func get_windup_ms() -> int:
		return __windup_ms.value
	func clear_windup_ms() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__windup_ms.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_windup_ms(value : int) -> void:
		__windup_ms.value = value
	
	var __root: PBField
	func has_root() -> bool:
		if __root.value != null:
			return true
		return false
	func get_root() -> bool:
		return __root.value
	func clear_root() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__root.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_root(value : bool) -> void:
		__root.value = value
	
	var __unlock_level: PBField
	func has_unlock_level() -> bool:
		if __unlock_level.value != null:
			return true
		return false
	func get_unlock_level() -> int:
		return __unlock_level.value
	func clear_unlock_level() -> void:
		data[7].state = PB_SERVICE_STATE.UNFILLED
		__unlock_level.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_unlock_level(value : int) -> void:
		__unlock_level.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class Extracted:
	func _init():
		var service
		
		__player_id = PBField.new("player_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __player_id
		data[__player_id.tag] = service
		
		__summary = PBField.new("summary", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __summary
		service.func_ref = Callable(self, "new_summary")
		data[__summary.tag] = service
		
	var data = {}
	
	var __player_id: PBField
	func has_player_id() -> bool:
		if __player_id.value != null:
			return true
		return false
	func get_player_id() -> int:
		return __player_id.value
	func clear_player_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__player_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_player_id(value : int) -> void:
		__player_id.value = value
	
	var __summary: PBField
	func has_summary() -> bool:
		if __summary.value != null:
			return true
		return false
	func get_summary() -> RunSummary:
		return __summary.value
	func clear_summary() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__summary.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_summary() -> RunSummary:
		__summary.value = RunSummary.new()
		return __summary.value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class RunEnded:
	func _init():
		var service
		
		__summary = PBField.new("summary", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __summary
		service.func_ref = Callable(self, "new_summary")
		data[__summary.tag] = service
		
	var data = {}
	
	var __summary: PBField
	func has_summary() -> bool:
		if __summary.value != null:
			return true
		return false
	func get_summary() -> RunSummary:
		return __summary.value
	func clear_summary() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__summary.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_summary() -> RunSummary:
		__summary.value = RunSummary.new()
		return __summary.value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class RunSummary:
	func _init():
		var service
		
		__extracted = PBField.new("extracted", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __extracted
		data[__extracted.tag] = service
		
		__run_xp = PBField.new("run_xp", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __run_xp
		data[__run_xp.tag] = service
		
		__banked_xp = PBField.new("banked_xp", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __banked_xp
		data[__banked_xp.tag] = service
		
		__levels_gained = PBField.new("levels_gained", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __levels_gained
		data[__levels_gained.tag] = service
		
		__character = PBField.new("character", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __character
		service.func_ref = Callable(self, "new_character")
		data[__character.tag] = service
		
		var __kept_default: Array[Item] = []
		__kept = PBField.new("kept", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 6, true, __kept_default)
		service = PBServiceField.new()
		service.field = __kept
		service.func_ref = Callable(self, "add_kept")
		data[__kept.tag] = service
		
		var __overflow_default: Array[Item] = []
		__overflow = PBField.new("overflow", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 7, true, __overflow_default)
		service = PBServiceField.new()
		service.field = __overflow
		service.func_ref = Callable(self, "add_overflow")
		data[__overflow.tag] = service
		
	var data = {}
	
	var __extracted: PBField
	func has_extracted() -> bool:
		if __extracted.value != null:
			return true
		return false
	func get_extracted() -> bool:
		return __extracted.value
	func clear_extracted() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__extracted.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_extracted(value : bool) -> void:
		__extracted.value = value
	
	var __run_xp: PBField
	func has_run_xp() -> bool:
		if __run_xp.value != null:
			return true
		return false
	func get_run_xp() -> int:
		return __run_xp.value
	func clear_run_xp() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__run_xp.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_run_xp(value : int) -> void:
		__run_xp.value = value
	
	var __banked_xp: PBField
	func has_banked_xp() -> bool:
		if __banked_xp.value != null:
			return true
		return false
	func get_banked_xp() -> int:
		return __banked_xp.value
	func clear_banked_xp() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__banked_xp.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_banked_xp(value : int) -> void:
		__banked_xp.value = value
	
	var __levels_gained: PBField
	func has_levels_gained() -> bool:
		if __levels_gained.value != null:
			return true
		return false
	func get_levels_gained() -> int:
		return __levels_gained.value
	func clear_levels_gained() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__levels_gained.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_levels_gained(value : int) -> void:
		__levels_gained.value = value
	
	var __character: PBField
	func has_character() -> bool:
		if __character.value != null:
			return true
		return false
	func get_character() -> CharacterInfo:
		return __character.value
	func clear_character() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__character.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_character() -> CharacterInfo:
		__character.value = CharacterInfo.new()
		return __character.value
	
	var __kept: PBField
	func get_kept() -> Array[Item]:
		return __kept.value
	func clear_kept() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__kept.value.clear()
	func add_kept() -> Item:
		var element = Item.new()
		__kept.value.append(element)
		return element
	
	var __overflow: PBField
	func get_overflow() -> Array[Item]:
		return __overflow.value
	func clear_overflow() -> void:
		data[7].state = PB_SERVICE_STATE.UNFILLED
		__overflow.value.clear()
	func add_overflow() -> Item:
		var element = Item.new()
		__overflow.value.append(element)
		return element
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class XpGained:
	func _init():
		var service
		
		__amount = PBField.new("amount", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __amount
		data[__amount.tag] = service
		
		__source = PBField.new("source", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __source
		data[__source.tag] = service
		
		__run_xp = PBField.new("run_xp", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __run_xp
		data[__run_xp.tag] = service
		
	var data = {}
	
	var __amount: PBField
	func has_amount() -> bool:
		if __amount.value != null:
			return true
		return false
	func get_amount() -> int:
		return __amount.value
	func clear_amount() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__amount.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_amount(value : int) -> void:
		__amount.value = value
	
	var __source: PBField
	func has_source() -> bool:
		if __source.value != null:
			return true
		return false
	func get_source() -> String:
		return __source.value
	func clear_source() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__source.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_source(value : String) -> void:
		__source.value = value
	
	var __run_xp: PBField
	func has_run_xp() -> bool:
		if __run_xp.value != null:
			return true
		return false
	func get_run_xp() -> int:
		return __run_xp.value
	func clear_run_xp() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__run_xp.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_run_xp(value : int) -> void:
		__run_xp.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class LevelUp:
	func _init():
		var service
		
		__level = PBField.new("level", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __level
		data[__level.tag] = service
		
		__class_info = PBField.new("class_info", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __class_info
		service.func_ref = Callable(self, "new_class_info")
		data[__class_info.tag] = service
		
	var data = {}
	
	var __level: PBField
	func has_level() -> bool:
		if __level.value != null:
			return true
		return false
	func get_level() -> int:
		return __level.value
	func clear_level() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__level.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_level(value : int) -> void:
		__level.value = value
	
	var __class_info: PBField
	func has_class_info() -> bool:
		if __class_info.value != null:
			return true
		return false
	func get_class_info() -> ClassInfo:
		return __class_info.value
	func clear_class_info() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__class_info.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_class_info() -> ClassInfo:
		__class_info.value = ClassInfo.new()
		return __class_info.value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class MoveInput:
	func _init():
		var service
		
		__seq = PBField.new("seq", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __seq
		data[__seq.tag] = service
		
		__direction = PBField.new("direction", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __direction
		service.func_ref = Callable(self, "new_direction")
		data[__direction.tag] = service
		
	var data = {}
	
	var __seq: PBField
	func has_seq() -> bool:
		if __seq.value != null:
			return true
		return false
	func get_seq() -> int:
		return __seq.value
	func clear_seq() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__seq.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_seq(value : int) -> void:
		__seq.value = value
	
	var __direction: PBField
	func has_direction() -> bool:
		if __direction.value != null:
			return true
		return false
	func get_direction() -> Vec2:
		return __direction.value
	func clear_direction() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__direction.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_direction() -> Vec2:
		__direction.value = Vec2.new()
		return __direction.value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class AttackRequest:
	func _init():
		var service
		
		__seq = PBField.new("seq", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __seq
		data[__seq.tag] = service
		
		__target_id = PBField.new("target_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __target_id
		data[__target_id.tag] = service
		
		__ability_id = PBField.new("ability_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __ability_id
		data[__ability_id.tag] = service
		
		__aim = PBField.new("aim", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __aim
		service.func_ref = Callable(self, "new_aim")
		data[__aim.tag] = service
		
	var data = {}
	
	var __seq: PBField
	func has_seq() -> bool:
		if __seq.value != null:
			return true
		return false
	func get_seq() -> int:
		return __seq.value
	func clear_seq() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__seq.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_seq(value : int) -> void:
		__seq.value = value
	
	var __target_id: PBField
	func has_target_id() -> bool:
		if __target_id.value != null:
			return true
		return false
	func get_target_id() -> int:
		return __target_id.value
	func clear_target_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__target_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_target_id(value : int) -> void:
		__target_id.value = value
	
	var __ability_id: PBField
	func has_ability_id() -> bool:
		if __ability_id.value != null:
			return true
		return false
	func get_ability_id() -> String:
		return __ability_id.value
	func clear_ability_id() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__ability_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_ability_id(value : String) -> void:
		__ability_id.value = value
	
	var __aim: PBField
	func has_aim() -> bool:
		if __aim.value != null:
			return true
		return false
	func get_aim() -> Vec2:
		return __aim.value
	func clear_aim() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__aim.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_aim() -> Vec2:
		__aim.value = Vec2.new()
		return __aim.value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class AbilityUsed:
	func _init():
		var service
		
		__source_id = PBField.new("source_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __source_id
		data[__source_id.tag] = service
		
		__ability_id = PBField.new("ability_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __ability_id
		data[__ability_id.tag] = service
		
		__aim = PBField.new("aim", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __aim
		service.func_ref = Callable(self, "new_aim")
		data[__aim.tag] = service
		
		__windup_ms = PBField.new("windup_ms", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __windup_ms
		data[__windup_ms.tag] = service
		
		__cooldown_ms = PBField.new("cooldown_ms", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __cooldown_ms
		data[__cooldown_ms.tag] = service
		
	var data = {}
	
	var __source_id: PBField
	func has_source_id() -> bool:
		if __source_id.value != null:
			return true
		return false
	func get_source_id() -> int:
		return __source_id.value
	func clear_source_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__source_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_source_id(value : int) -> void:
		__source_id.value = value
	
	var __ability_id: PBField
	func has_ability_id() -> bool:
		if __ability_id.value != null:
			return true
		return false
	func get_ability_id() -> String:
		return __ability_id.value
	func clear_ability_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__ability_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_ability_id(value : String) -> void:
		__ability_id.value = value
	
	var __aim: PBField
	func has_aim() -> bool:
		if __aim.value != null:
			return true
		return false
	func get_aim() -> Vec2:
		return __aim.value
	func clear_aim() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__aim.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_aim() -> Vec2:
		__aim.value = Vec2.new()
		return __aim.value
	
	var __windup_ms: PBField
	func has_windup_ms() -> bool:
		if __windup_ms.value != null:
			return true
		return false
	func get_windup_ms() -> int:
		return __windup_ms.value
	func clear_windup_ms() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__windup_ms.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_windup_ms(value : int) -> void:
		__windup_ms.value = value
	
	var __cooldown_ms: PBField
	func has_cooldown_ms() -> bool:
		if __cooldown_ms.value != null:
			return true
		return false
	func get_cooldown_ms() -> int:
		return __cooldown_ms.value
	func clear_cooldown_ms() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__cooldown_ms.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_cooldown_ms(value : int) -> void:
		__cooldown_ms.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class AbilityRejected:
	func _init():
		var service
		
		__seq = PBField.new("seq", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __seq
		data[__seq.tag] = service
		
		__ability_id = PBField.new("ability_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __ability_id
		data[__ability_id.tag] = service
		
		__reason = PBField.new("reason", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __reason
		data[__reason.tag] = service
		
	var data = {}
	
	var __seq: PBField
	func has_seq() -> bool:
		if __seq.value != null:
			return true
		return false
	func get_seq() -> int:
		return __seq.value
	func clear_seq() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__seq.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_seq(value : int) -> void:
		__seq.value = value
	
	var __ability_id: PBField
	func has_ability_id() -> bool:
		if __ability_id.value != null:
			return true
		return false
	func get_ability_id() -> String:
		return __ability_id.value
	func clear_ability_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__ability_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_ability_id(value : String) -> void:
		__ability_id.value = value
	
	var __reason: PBField
	func has_reason() -> bool:
		if __reason.value != null:
			return true
		return false
	func get_reason() -> String:
		return __reason.value
	func clear_reason() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__reason.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_reason(value : String) -> void:
		__reason.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class DamageEvent:
	func _init():
		var service
		
		__source_id = PBField.new("source_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __source_id
		data[__source_id.tag] = service
		
		__target_id = PBField.new("target_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __target_id
		data[__target_id.tag] = service
		
		__amount = PBField.new("amount", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __amount
		data[__amount.tag] = service
		
		__target_health = PBField.new("target_health", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __target_health
		data[__target_health.tag] = service
		
		__killed = PBField.new("killed", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __killed
		data[__killed.tag] = service
		
	var data = {}
	
	var __source_id: PBField
	func has_source_id() -> bool:
		if __source_id.value != null:
			return true
		return false
	func get_source_id() -> int:
		return __source_id.value
	func clear_source_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__source_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_source_id(value : int) -> void:
		__source_id.value = value
	
	var __target_id: PBField
	func has_target_id() -> bool:
		if __target_id.value != null:
			return true
		return false
	func get_target_id() -> int:
		return __target_id.value
	func clear_target_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__target_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_target_id(value : int) -> void:
		__target_id.value = value
	
	var __amount: PBField
	func has_amount() -> bool:
		if __amount.value != null:
			return true
		return false
	func get_amount() -> int:
		return __amount.value
	func clear_amount() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__amount.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_amount(value : int) -> void:
		__amount.value = value
	
	var __target_health: PBField
	func has_target_health() -> bool:
		if __target_health.value != null:
			return true
		return false
	func get_target_health() -> int:
		return __target_health.value
	func clear_target_health() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__target_health.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_target_health(value : int) -> void:
		__target_health.value = value
	
	var __killed: PBField
	func has_killed() -> bool:
		if __killed.value != null:
			return true
		return false
	func get_killed() -> bool:
		return __killed.value
	func clear_killed() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__killed.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_killed(value : bool) -> void:
		__killed.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
enum EntityKind {
	UNSPECIFIED = 0,
	PLAYER = 1,
	ENEMY = 2,
	CHEST = 3,
	LOOT = 4,
	PROJECTILE = 5
}

enum EnemyState {
	UNSPECIFIED = 0,
	IDLE = 1,
	PATROL = 2,
	DETECT = 3,
	CHASE = 4,
	ATTACK = 5,
	SEARCH = 6,
	RETREAT = 7,
	RETURN = 8,
	CHARGE = 9,
	DEAD = 10
}

class EntityState:
	func _init():
		var service
		
		__id = PBField.new("id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __id
		data[__id.tag] = service
		
		__kind = PBField.new("kind", PB_DATA_TYPE.ENUM, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM])
		service = PBServiceField.new()
		service.field = __kind
		data[__kind.tag] = service
		
		__position = PBField.new("position", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __position
		service.func_ref = Callable(self, "new_position")
		data[__position.tag] = service
		
		__velocity = PBField.new("velocity", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __velocity
		service.func_ref = Callable(self, "new_velocity")
		data[__velocity.tag] = service
		
		__health = PBField.new("health", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __health
		data[__health.tag] = service
		
		__extract_progress = PBField.new("extract_progress", PB_DATA_TYPE.FLOAT, PB_RULE.OPTIONAL, 6, true, DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT])
		service = PBServiceField.new()
		service.field = __extract_progress
		data[__extract_progress.tag] = service
		
		__max_health = PBField.new("max_health", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 7, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __max_health
		data[__max_health.tag] = service
		
		__last_input_seq = PBField.new("last_input_seq", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 8, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __last_input_seq
		data[__last_input_seq.tag] = service
		
		__max_speed = PBField.new("max_speed", PB_DATA_TYPE.FLOAT, PB_RULE.OPTIONAL, 9, true, DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT])
		service = PBServiceField.new()
		service.field = __max_speed
		data[__max_speed.tag] = service
		
		__item_count = PBField.new("item_count", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 10, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __item_count
		data[__item_count.tag] = service
		
		__archetype = PBField.new("archetype", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 11, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __archetype
		data[__archetype.tag] = service
		
		__enemy_state = PBField.new("enemy_state", PB_DATA_TYPE.ENUM, PB_RULE.OPTIONAL, 12, true, DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM])
		service = PBServiceField.new()
		service.field = __enemy_state
		data[__enemy_state.tag] = service
		
		__aim = PBField.new("aim", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 13, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __aim
		service.func_ref = Callable(self, "new_aim")
		data[__aim.tag] = service
		
		__class_id = PBField.new("class_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 14, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __class_id
		data[__class_id.tag] = service
		
		__resource = PBField.new("resource", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 15, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __resource
		data[__resource.tag] = service
		
		__max_resource = PBField.new("max_resource", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 16, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __max_resource
		data[__max_resource.tag] = service
		
		__level = PBField.new("level", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 17, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __level
		data[__level.tag] = service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 18, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
	var data = {}
	
	var __id: PBField
	func has_id() -> bool:
		if __id.value != null:
			return true
		return false
	func get_id() -> int:
		return __id.value
	func clear_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_id(value : int) -> void:
		__id.value = value
	
	var __kind: PBField
	func has_kind() -> bool:
		if __kind.value != null:
			return true
		return false
	func get_kind():
		return __kind.value
	func clear_kind() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__kind.value = DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM]
	func set_kind(value) -> void:
		__kind.value = value
	
	var __position: PBField
	func has_position() -> bool:
		if __position.value != null:
			return true
		return false
	func get_position() -> Vec2:
		return __position.value
	func clear_position() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__position.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_position() -> Vec2:
		__position.value = Vec2.new()
		return __position.value
	
	var __velocity: PBField
	func has_velocity() -> bool:
		if __velocity.value != null:
			return true
		return false
	func get_velocity() -> Vec2:
		return __velocity.value
	func clear_velocity() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__velocity.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_velocity() -> Vec2:
		__velocity.value = Vec2.new()
		return __velocity.value
	
	var __health: PBField
	func has_health() -> bool:
		if __health.value != null:
			return true
		return false
	func get_health() -> int:
		return __health.value
	func clear_health() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__health.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_health(value : int) -> void:
		__health.value = value
	
	var __extract_progress: PBField
	func has_extract_progress() -> bool:
		if __extract_progress.value != null:
			return true
		return false
	func get_extract_progress() -> float:
		return __extract_progress.value
	func clear_extract_progress() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__extract_progress.value = DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT]
	func set_extract_progress(value : float) -> void:
		__extract_progress.value = value
	
	var __max_health: PBField
	func has_max_health() -> bool:
		if __max_health.value != null:
			return true
		return false
	func get_max_health() -> int:
		return __max_health.value
	func clear_max_health() -> void:
		data[7].state = PB_SERVICE_STATE.UNFILLED
		__max_health.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_max_health(value : int) -> void:
		__max_health.value = value
	
	var __last_input_seq: PBField
	func has_last_input_seq() -> bool:
		if __last_input_seq.value != null:
			return true
		return false
	func get_last_input_seq() -> int:
		return __last_input_seq.value
	func clear_last_input_seq() -> void:
		data[8].state = PB_SERVICE_STATE.UNFILLED
		__last_input_seq.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_last_input_seq(value : int) -> void:
		__last_input_seq.value = value
	
	var __max_speed: PBField
	func has_max_speed() -> bool:
		if __max_speed.value != null:
			return true
		return false
	func get_max_speed() -> float:
		return __max_speed.value
	func clear_max_speed() -> void:
		data[9].state = PB_SERVICE_STATE.UNFILLED
		__max_speed.value = DEFAULT_VALUES_3[PB_DATA_TYPE.FLOAT]
	func set_max_speed(value : float) -> void:
		__max_speed.value = value
	
	var __item_count: PBField
	func has_item_count() -> bool:
		if __item_count.value != null:
			return true
		return false
	func get_item_count() -> int:
		return __item_count.value
	func clear_item_count() -> void:
		data[10].state = PB_SERVICE_STATE.UNFILLED
		__item_count.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_item_count(value : int) -> void:
		__item_count.value = value
	
	var __archetype: PBField
	func has_archetype() -> bool:
		if __archetype.value != null:
			return true
		return false
	func get_archetype() -> String:
		return __archetype.value
	func clear_archetype() -> void:
		data[11].state = PB_SERVICE_STATE.UNFILLED
		__archetype.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_archetype(value : String) -> void:
		__archetype.value = value
	
	var __enemy_state: PBField
	func has_enemy_state() -> bool:
		if __enemy_state.value != null:
			return true
		return false
	func get_enemy_state():
		return __enemy_state.value
	func clear_enemy_state() -> void:
		data[12].state = PB_SERVICE_STATE.UNFILLED
		__enemy_state.value = DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM]
	func set_enemy_state(value) -> void:
		__enemy_state.value = value
	
	var __aim: PBField
	func has_aim() -> bool:
		if __aim.value != null:
			return true
		return false
	func get_aim() -> Vec2:
		return __aim.value
	func clear_aim() -> void:
		data[13].state = PB_SERVICE_STATE.UNFILLED
		__aim.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_aim() -> Vec2:
		__aim.value = Vec2.new()
		return __aim.value
	
	var __class_id: PBField
	func has_class_id() -> bool:
		if __class_id.value != null:
			return true
		return false
	func get_class_id() -> String:
		return __class_id.value
	func clear_class_id() -> void:
		data[14].state = PB_SERVICE_STATE.UNFILLED
		__class_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_class_id(value : String) -> void:
		__class_id.value = value
	
	var __resource: PBField
	func has_resource() -> bool:
		if __resource.value != null:
			return true
		return false
	func get_resource() -> int:
		return __resource.value
	func clear_resource() -> void:
		data[15].state = PB_SERVICE_STATE.UNFILLED
		__resource.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_resource(value : int) -> void:
		__resource.value = value
	
	var __max_resource: PBField
	func has_max_resource() -> bool:
		if __max_resource.value != null:
			return true
		return false
	func get_max_resource() -> int:
		return __max_resource.value
	func clear_max_resource() -> void:
		data[16].state = PB_SERVICE_STATE.UNFILLED
		__max_resource.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_max_resource(value : int) -> void:
		__max_resource.value = value
	
	var __level: PBField
	func has_level() -> bool:
		if __level.value != null:
			return true
		return false
	func get_level() -> int:
		return __level.value
	func clear_level() -> void:
		data[17].state = PB_SERVICE_STATE.UNFILLED
		__level.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_level(value : int) -> void:
		__level.value = value
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[18].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class WorldSnapshot:
	func _init():
		var service
		
		__tick = PBField.new("tick", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __tick
		data[__tick.tag] = service
		
		var __entities_default: Array[EntityState] = []
		__entities = PBField.new("entities", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 2, true, __entities_default)
		service = PBServiceField.new()
		service.field = __entities
		service.func_ref = Callable(self, "add_entities")
		data[__entities.tag] = service
		
	var data = {}
	
	var __tick: PBField
	func has_tick() -> bool:
		if __tick.value != null:
			return true
		return false
	func get_tick() -> int:
		return __tick.value
	func clear_tick() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__tick.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_tick(value : int) -> void:
		__tick.value = value
	
	var __entities: PBField
	func get_entities() -> Array[EntityState]:
		return __entities.value
	func clear_entities() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__entities.value.clear()
	func add_entities() -> EntityState:
		var element = EntityState.new()
		__entities.value.append(element)
		return element
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class InstanceLayout:
	func _init():
		var service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
		__bounds = PBField.new("bounds", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __bounds
		service.func_ref = Callable(self, "new_bounds")
		data[__bounds.tag] = service
		
		var __walls_default: Array[Rect] = []
		__walls = PBField.new("walls", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 3, true, __walls_default)
		service = PBServiceField.new()
		service.field = __walls
		service.func_ref = Callable(self, "add_walls")
		data[__walls.tag] = service
		
		__extraction = PBField.new("extraction", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __extraction
		service.func_ref = Callable(self, "new_extraction")
		data[__extraction.tag] = service
		
		__entrance = PBField.new("entrance", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __entrance
		service.func_ref = Callable(self, "new_entrance")
		data[__entrance.tag] = service
		
		__stash = PBField.new("stash", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 6, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __stash
		service.func_ref = Callable(self, "new_stash")
		data[__stash.tag] = service
		
	var data = {}
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	var __bounds: PBField
	func has_bounds() -> bool:
		if __bounds.value != null:
			return true
		return false
	func get_bounds() -> Rect:
		return __bounds.value
	func clear_bounds() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__bounds.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_bounds() -> Rect:
		__bounds.value = Rect.new()
		return __bounds.value
	
	var __walls: PBField
	func get_walls() -> Array[Rect]:
		return __walls.value
	func clear_walls() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__walls.value.clear()
	func add_walls() -> Rect:
		var element = Rect.new()
		__walls.value.append(element)
		return element
	
	var __extraction: PBField
	func has_extraction() -> bool:
		if __extraction.value != null:
			return true
		return false
	func get_extraction() -> Rect:
		return __extraction.value
	func clear_extraction() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__extraction.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_extraction() -> Rect:
		__extraction.value = Rect.new()
		return __extraction.value
	
	var __entrance: PBField
	func has_entrance() -> bool:
		if __entrance.value != null:
			return true
		return false
	func get_entrance() -> Rect:
		return __entrance.value
	func clear_entrance() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__entrance.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_entrance() -> Rect:
		__entrance.value = Rect.new()
		return __entrance.value
	
	var __stash: PBField
	func has_stash() -> bool:
		if __stash.value != null:
			return true
		return false
	func get_stash() -> Vec2:
		return __stash.value
	func clear_stash() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__stash.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_stash() -> Vec2:
		__stash.value = Vec2.new()
		return __stash.value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
enum Rarity {
	UNSPECIFIED = 0,
	COMMON = 1,
	UNCOMMON = 2,
	RARE = 3,
	EPIC = 4,
	UNIQUE = 5
}

class Item:
	func _init():
		var service
		
		__id = PBField.new("id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __id
		data[__id.tag] = service
		
		__item_def_id = PBField.new("item_def_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_def_id
		data[__item_def_id.tag] = service
		
		__quantity = PBField.new("quantity", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __quantity
		data[__quantity.tag] = service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
		__slot = PBField.new("slot", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 5, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __slot
		data[__slot.tag] = service
		
		__rarity = PBField.new("rarity", PB_DATA_TYPE.ENUM, PB_RULE.OPTIONAL, 6, true, DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM])
		service = PBServiceField.new()
		service.field = __rarity
		data[__rarity.tag] = service
		
		__level = PBField.new("level", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 7, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __level
		data[__level.tag] = service
		
		var __base_default: Array[ItemStat] = []
		__base = PBField.new("base", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 8, true, __base_default)
		service = PBServiceField.new()
		service.field = __base
		service.func_ref = Callable(self, "add_base")
		data[__base.tag] = service
		
		var __affixes_default: Array[ItemAffix] = []
		__affixes = PBField.new("affixes", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 9, true, __affixes_default)
		service = PBServiceField.new()
		service.field = __affixes
		service.func_ref = Callable(self, "add_affixes")
		data[__affixes.tag] = service
		
	var data = {}
	
	var __id: PBField
	func has_id() -> bool:
		if __id.value != null:
			return true
		return false
	func get_id() -> String:
		return __id.value
	func clear_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_id(value : String) -> void:
		__id.value = value
	
	var __item_def_id: PBField
	func has_item_def_id() -> bool:
		if __item_def_id.value != null:
			return true
		return false
	func get_item_def_id() -> String:
		return __item_def_id.value
	func clear_item_def_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__item_def_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_def_id(value : String) -> void:
		__item_def_id.value = value
	
	var __quantity: PBField
	func has_quantity() -> bool:
		if __quantity.value != null:
			return true
		return false
	func get_quantity() -> int:
		return __quantity.value
	func clear_quantity() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__quantity.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_quantity(value : int) -> void:
		__quantity.value = value
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	var __slot: PBField
	func has_slot() -> bool:
		if __slot.value != null:
			return true
		return false
	func get_slot() -> String:
		return __slot.value
	func clear_slot() -> void:
		data[5].state = PB_SERVICE_STATE.UNFILLED
		__slot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_slot(value : String) -> void:
		__slot.value = value
	
	var __rarity: PBField
	func has_rarity() -> bool:
		if __rarity.value != null:
			return true
		return false
	func get_rarity():
		return __rarity.value
	func clear_rarity() -> void:
		data[6].state = PB_SERVICE_STATE.UNFILLED
		__rarity.value = DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM]
	func set_rarity(value) -> void:
		__rarity.value = value
	
	var __level: PBField
	func has_level() -> bool:
		if __level.value != null:
			return true
		return false
	func get_level() -> int:
		return __level.value
	func clear_level() -> void:
		data[7].state = PB_SERVICE_STATE.UNFILLED
		__level.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_level(value : int) -> void:
		__level.value = value
	
	var __base: PBField
	func get_base() -> Array[ItemStat]:
		return __base.value
	func clear_base() -> void:
		data[8].state = PB_SERVICE_STATE.UNFILLED
		__base.value.clear()
	func add_base() -> ItemStat:
		var element = ItemStat.new()
		__base.value.append(element)
		return element
	
	var __affixes: PBField
	func get_affixes() -> Array[ItemAffix]:
		return __affixes.value
	func clear_affixes() -> void:
		data[9].state = PB_SERVICE_STATE.UNFILLED
		__affixes.value.clear()
	func add_affixes() -> ItemAffix:
		var element = ItemAffix.new()
		__affixes.value.append(element)
		return element
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class ItemStat:
	func _init():
		var service
		
		__stat = PBField.new("stat", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __stat
		data[__stat.tag] = service
		
		__value = PBField.new("value", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __value
		data[__value.tag] = service
		
	var data = {}
	
	var __stat: PBField
	func has_stat() -> bool:
		if __stat.value != null:
			return true
		return false
	func get_stat() -> String:
		return __stat.value
	func clear_stat() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__stat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_stat(value : String) -> void:
		__stat.value = value
	
	var __value: PBField
	func has_value() -> bool:
		if __value.value != null:
			return true
		return false
	func get_value() -> int:
		return __value.value
	func clear_value() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__value.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_value(value : int) -> void:
		__value.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class ItemAffix:
	func _init():
		var service
		
		__name = PBField.new("name", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __name
		data[__name.tag] = service
		
		__stat = PBField.new("stat", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __stat
		data[__stat.tag] = service
		
		__value = PBField.new("value", PB_DATA_TYPE.INT32, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.INT32])
		service = PBServiceField.new()
		service.field = __value
		data[__value.tag] = service
		
		__tier = PBField.new("tier", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __tier
		data[__tier.tag] = service
		
	var data = {}
	
	var __name: PBField
	func has_name() -> bool:
		if __name.value != null:
			return true
		return false
	func get_name() -> String:
		return __name.value
	func clear_name() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__name.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_name(value : String) -> void:
		__name.value = value
	
	var __stat: PBField
	func has_stat() -> bool:
		if __stat.value != null:
			return true
		return false
	func get_stat() -> String:
		return __stat.value
	func clear_stat() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__stat.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_stat(value : String) -> void:
		__stat.value = value
	
	var __value: PBField
	func has_value() -> bool:
		if __value.value != null:
			return true
		return false
	func get_value() -> int:
		return __value.value
	func clear_value() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__value.value = DEFAULT_VALUES_3[PB_DATA_TYPE.INT32]
	func set_value(value : int) -> void:
		__value.value = value
	
	var __tier: PBField
	func has_tier() -> bool:
		if __tier.value != null:
			return true
		return false
	func get_tier() -> int:
		return __tier.value
	func clear_tier() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__tier.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_tier(value : int) -> void:
		__tier.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class EquipmentUpdated:
	func _init():
		var service
		
		var __items_default: Array[Item] = []
		__items = PBField.new("items", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 1, true, __items_default)
		service = PBServiceField.new()
		service.field = __items
		service.func_ref = Callable(self, "add_items")
		data[__items.tag] = service
		
		var __stats_default: Array[ItemStat] = []
		__stats = PBField.new("stats", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 2, true, __stats_default)
		service = PBServiceField.new()
		service.field = __stats
		service.func_ref = Callable(self, "add_stats")
		data[__stats.tag] = service
		
	var data = {}
	
	var __items: PBField
	func get_items() -> Array[Item]:
		return __items.value
	func clear_items() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__items.value.clear()
	func add_items() -> Item:
		var element = Item.new()
		__items.value.append(element)
		return element
	
	var __stats: PBField
	func get_stats() -> Array[ItemStat]:
		return __stats.value
	func clear_stats() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__stats.value.clear()
	func add_stats() -> ItemStat:
		var element = ItemStat.new()
		__stats.value.append(element)
		return element
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class InventoryUpdated:
	func _init():
		var service
		
		var __items_default: Array[Item] = []
		__items = PBField.new("items", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 1, true, __items_default)
		service = PBServiceField.new()
		service.field = __items
		service.func_ref = Callable(self, "add_items")
		data[__items.tag] = service
		
		__capacity = PBField.new("capacity", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __capacity
		data[__capacity.tag] = service
		
	var data = {}
	
	var __items: PBField
	func get_items() -> Array[Item]:
		return __items.value
	func clear_items() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__items.value.clear()
	func add_items() -> Item:
		var element = Item.new()
		__items.value.append(element)
		return element
	
	var __capacity: PBField
	func has_capacity() -> bool:
		if __capacity.value != null:
			return true
		return false
	func get_capacity() -> int:
		return __capacity.value
	func clear_capacity() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__capacity.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_capacity(value : int) -> void:
		__capacity.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class PickupRequest:
	func _init():
		var service
		
		__target_id = PBField.new("target_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __target_id
		data[__target_id.tag] = service
		
		__item_id = PBField.new("item_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_id
		data[__item_id.tag] = service
		
	var data = {}
	
	var __target_id: PBField
	func has_target_id() -> bool:
		if __target_id.value != null:
			return true
		return false
	func get_target_id() -> int:
		return __target_id.value
	func clear_target_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__target_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_target_id(value : int) -> void:
		__target_id.value = value
	
	var __item_id: PBField
	func has_item_id() -> bool:
		if __item_id.value != null:
			return true
		return false
	func get_item_id() -> String:
		return __item_id.value
	func clear_item_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__item_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_id(value : String) -> void:
		__item_id.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class PickupRejected:
	func _init():
		var service
		
		__target_id = PBField.new("target_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __target_id
		data[__target_id.tag] = service
		
		__item_id = PBField.new("item_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_id
		data[__item_id.tag] = service
		
		__reason = PBField.new("reason", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __reason
		data[__reason.tag] = service
		
	var data = {}
	
	var __target_id: PBField
	func has_target_id() -> bool:
		if __target_id.value != null:
			return true
		return false
	func get_target_id() -> int:
		return __target_id.value
	func clear_target_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__target_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_target_id(value : int) -> void:
		__target_id.value = value
	
	var __item_id: PBField
	func has_item_id() -> bool:
		if __item_id.value != null:
			return true
		return false
	func get_item_id() -> String:
		return __item_id.value
	func clear_item_id() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__item_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_id(value : String) -> void:
		__item_id.value = value
	
	var __reason: PBField
	func has_reason() -> bool:
		if __reason.value != null:
			return true
		return false
	func get_reason() -> String:
		return __reason.value
	func clear_reason() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__reason.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_reason(value : String) -> void:
		__reason.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class LootOpenRequest:
	func _init():
		var service
		
		__target_id = PBField.new("target_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __target_id
		data[__target_id.tag] = service
		
	var data = {}
	
	var __target_id: PBField
	func has_target_id() -> bool:
		if __target_id.value != null:
			return true
		return false
	func get_target_id() -> int:
		return __target_id.value
	func clear_target_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__target_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_target_id(value : int) -> void:
		__target_id.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class LootCloseRequest:
	func _init():
		var service
		
		__target_id = PBField.new("target_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __target_id
		data[__target_id.tag] = service
		
	var data = {}
	
	var __target_id: PBField
	func has_target_id() -> bool:
		if __target_id.value != null:
			return true
		return false
	func get_target_id() -> int:
		return __target_id.value
	func clear_target_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__target_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_target_id(value : int) -> void:
		__target_id.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class LootContents:
	func _init():
		var service
		
		__target_id = PBField.new("target_id", PB_DATA_TYPE.UINT64, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64])
		service = PBServiceField.new()
		service.field = __target_id
		data[__target_id.tag] = service
		
		var __items_default: Array[Item] = []
		__items = PBField.new("items", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 2, true, __items_default)
		service = PBServiceField.new()
		service.field = __items
		service.func_ref = Callable(self, "add_items")
		data[__items.tag] = service
		
		__closed = PBField.new("closed", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __closed
		data[__closed.tag] = service
		
	var data = {}
	
	var __target_id: PBField
	func has_target_id() -> bool:
		if __target_id.value != null:
			return true
		return false
	func get_target_id() -> int:
		return __target_id.value
	func clear_target_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__target_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT64]
	func set_target_id(value : int) -> void:
		__target_id.value = value
	
	var __items: PBField
	func get_items() -> Array[Item]:
		return __items.value
	func clear_items() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__items.value.clear()
	func add_items() -> Item:
		var element = Item.new()
		__items.value.append(element)
		return element
	
	var __closed: PBField
	func has_closed() -> bool:
		if __closed.value != null:
			return true
		return false
	func get_closed() -> bool:
		return __closed.value
	func clear_closed() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__closed.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_closed(value : bool) -> void:
		__closed.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class EquipRequest:
	func _init():
		var service
		
		__item_id = PBField.new("item_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_id
		data[__item_id.tag] = service
		
	var data = {}
	
	var __item_id: PBField
	func has_item_id() -> bool:
		if __item_id.value != null:
			return true
		return false
	func get_item_id() -> String:
		return __item_id.value
	func clear_item_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__item_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_id(value : String) -> void:
		__item_id.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class UnequipRequest:
	func _init():
		var service
		
		__slot = PBField.new("slot", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __slot
		data[__slot.tag] = service
		
	var data = {}
	
	var __slot: PBField
	func has_slot() -> bool:
		if __slot.value != null:
			return true
		return false
	func get_slot() -> String:
		return __slot.value
	func clear_slot() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__slot.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_slot(value : String) -> void:
		__slot.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class DropRequest:
	func _init():
		var service
		
		__item_id = PBField.new("item_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_id
		data[__item_id.tag] = service
		
	var data = {}
	
	var __item_id: PBField
	func has_item_id() -> bool:
		if __item_id.value != null:
			return true
		return false
	func get_item_id() -> String:
		return __item_id.value
	func clear_item_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__item_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_id(value : String) -> void:
		__item_id.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class UseRequest:
	func _init():
		var service
		
		__item_id = PBField.new("item_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_id
		data[__item_id.tag] = service
		
	var data = {}
	
	var __item_id: PBField
	func has_item_id() -> bool:
		if __item_id.value != null:
			return true
		return false
	func get_item_id() -> String:
		return __item_id.value
	func clear_item_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__item_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_id(value : String) -> void:
		__item_id.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class Lobby:
	func _init():
		var service
		
		__character = PBField.new("character", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __character
		service.func_ref = Callable(self, "new_character")
		data[__character.tag] = service
		
		__class_info = PBField.new("class_info", PB_DATA_TYPE.MESSAGE, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE])
		service = PBServiceField.new()
		service.field = __class_info
		service.func_ref = Callable(self, "new_class_info")
		data[__class_info.tag] = service
		
		__saving = PBField.new("saving", PB_DATA_TYPE.BOOL, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL])
		service = PBServiceField.new()
		service.field = __saving
		data[__saving.tag] = service
		
	var data = {}
	
	var __character: PBField
	func has_character() -> bool:
		if __character.value != null:
			return true
		return false
	func get_character() -> CharacterInfo:
		return __character.value
	func clear_character() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__character.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_character() -> CharacterInfo:
		__character.value = CharacterInfo.new()
		return __character.value
	
	var __class_info: PBField
	func has_class_info() -> bool:
		if __class_info.value != null:
			return true
		return false
	func get_class_info() -> ClassInfo:
		return __class_info.value
	func clear_class_info() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__class_info.value = DEFAULT_VALUES_3[PB_DATA_TYPE.MESSAGE]
	func new_class_info() -> ClassInfo:
		__class_info.value = ClassInfo.new()
		return __class_info.value
	
	var __saving: PBField
	func has_saving() -> bool:
		if __saving.value != null:
			return true
		return false
	func get_saving() -> bool:
		return __saving.value
	func clear_saving() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__saving.value = DEFAULT_VALUES_3[PB_DATA_TYPE.BOOL]
	func set_saving(value : bool) -> void:
		__saving.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class StashContents:
	func _init():
		var service
		
		var __items_default: Array[Item] = []
		__items = PBField.new("items", PB_DATA_TYPE.MESSAGE, PB_RULE.REPEATED, 1, true, __items_default)
		service = PBServiceField.new()
		service.field = __items
		service.func_ref = Callable(self, "add_items")
		data[__items.tag] = service
		
		__capacity = PBField.new("capacity", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __capacity
		data[__capacity.tag] = service
		
	var data = {}
	
	var __items: PBField
	func get_items() -> Array[Item]:
		return __items.value
	func clear_items() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__items.value.clear()
	func add_items() -> Item:
		var element = Item.new()
		__items.value.append(element)
		return element
	
	var __capacity: PBField
	func has_capacity() -> bool:
		if __capacity.value != null:
			return true
		return false
	func get_capacity() -> int:
		return __capacity.value
	func clear_capacity() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__capacity.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_capacity(value : int) -> void:
		__capacity.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
enum ItemPlace {
	UNSPECIFIED = 0,
	STASH = 1,
	BAG = 2,
	GEAR = 3
}

class StashMoveRequest:
	func _init():
		var service
		
		__item_id = PBField.new("item_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_id
		data[__item_id.tag] = service
		
		__to = PBField.new("to", PB_DATA_TYPE.ENUM, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM])
		service = PBServiceField.new()
		service.field = __to
		data[__to.tag] = service
		
	var data = {}
	
	var __item_id: PBField
	func has_item_id() -> bool:
		if __item_id.value != null:
			return true
		return false
	func get_item_id() -> String:
		return __item_id.value
	func clear_item_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__item_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_id(value : String) -> void:
		__item_id.value = value
	
	var __to: PBField
	func has_to() -> bool:
		if __to.value != null:
			return true
		return false
	func get_to():
		return __to.value
	func clear_to() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__to.value = DEFAULT_VALUES_3[PB_DATA_TYPE.ENUM]
	func set_to(value) -> void:
		__to.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class StashMoveRejected:
	func _init():
		var service
		
		__item_id = PBField.new("item_id", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __item_id
		data[__item_id.tag] = service
		
		__reason = PBField.new("reason", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __reason
		data[__reason.tag] = service
		
	var data = {}
	
	var __item_id: PBField
	func has_item_id() -> bool:
		if __item_id.value != null:
			return true
		return false
	func get_item_id() -> String:
		return __item_id.value
	func clear_item_id() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__item_id.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_item_id(value : String) -> void:
		__item_id.value = value
	
	var __reason: PBField
	func has_reason() -> bool:
		if __reason.value != null:
			return true
		return false
	func get_reason() -> String:
		return __reason.value
	func clear_reason() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__reason.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_reason(value : String) -> void:
		__reason.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class EnterInstanceRejected:
	func _init():
		var service
		
		__reason = PBField.new("reason", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __reason
		data[__reason.tag] = service
		
	var data = {}
	
	var __reason: PBField
	func has_reason() -> bool:
		if __reason.value != null:
			return true
		return false
	func get_reason() -> String:
		return __reason.value
	func clear_reason() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__reason.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_reason(value : String) -> void:
		__reason.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class InstanceTicket:
	func _init():
		var service
		
		__url = PBField.new("url", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __url
		data[__url.tag] = service
		
		__token = PBField.new("token", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __token
		data[__token.tag] = service
		
		__instance = PBField.new("instance", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __instance
		data[__instance.tag] = service
		
		__expires_in_ms = PBField.new("expires_in_ms", PB_DATA_TYPE.UINT32, PB_RULE.OPTIONAL, 4, true, DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32])
		service = PBServiceField.new()
		service.field = __expires_in_ms
		data[__expires_in_ms.tag] = service
		
	var data = {}
	
	var __url: PBField
	func has_url() -> bool:
		if __url.value != null:
			return true
		return false
	func get_url() -> String:
		return __url.value
	func clear_url() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__url.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_url(value : String) -> void:
		__url.value = value
	
	var __token: PBField
	func has_token() -> bool:
		if __token.value != null:
			return true
		return false
	func get_token() -> String:
		return __token.value
	func clear_token() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__token.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_token(value : String) -> void:
		__token.value = value
	
	var __instance: PBField
	func has_instance() -> bool:
		if __instance.value != null:
			return true
		return false
	func get_instance() -> String:
		return __instance.value
	func clear_instance() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__instance.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_instance(value : String) -> void:
		__instance.value = value
	
	var __expires_in_ms: PBField
	func has_expires_in_ms() -> bool:
		if __expires_in_ms.value != null:
			return true
		return false
	func get_expires_in_ms() -> int:
		return __expires_in_ms.value
	func clear_expires_in_ms() -> void:
		data[4].state = PB_SERVICE_STATE.UNFILLED
		__expires_in_ms.value = DEFAULT_VALUES_3[PB_DATA_TYPE.UINT32]
	func set_expires_in_ms(value : int) -> void:
		__expires_in_ms.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class TownTicket:
	func _init():
		var service
		
		__url = PBField.new("url", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __url
		data[__url.tag] = service
		
	var data = {}
	
	var __url: PBField
	func has_url() -> bool:
		if __url.value != null:
			return true
		return false
	func get_url() -> String:
		return __url.value
	func clear_url() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__url.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_url(value : String) -> void:
		__url.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class InstanceJoinRequest:
	func _init():
		var service
		
		__token = PBField.new("token", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __token
		data[__token.tag] = service
		
	var data = {}
	
	var __token: PBField
	func has_token() -> bool:
		if __token.value != null:
			return true
		return false
	func get_token() -> String:
		return __token.value
	func clear_token() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__token.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_token(value : String) -> void:
		__token.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class ChatSend:
	func _init():
		var service
		
		__text = PBField.new("text", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __text
		data[__text.tag] = service
		
	var data = {}
	
	var __text: PBField
	func has_text() -> bool:
		if __text.value != null:
			return true
		return false
	func get_text() -> String:
		return __text.value
	func clear_text() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__text.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_text(value : String) -> void:
		__text.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class ChatMessage:
	func _init():
		var service
		
		__channel = PBField.new("channel", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 1, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __channel
		data[__channel.tag] = service
		
		__sender = PBField.new("sender", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 2, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __sender
		data[__sender.tag] = service
		
		__text = PBField.new("text", PB_DATA_TYPE.STRING, PB_RULE.OPTIONAL, 3, true, DEFAULT_VALUES_3[PB_DATA_TYPE.STRING])
		service = PBServiceField.new()
		service.field = __text
		data[__text.tag] = service
		
	var data = {}
	
	var __channel: PBField
	func has_channel() -> bool:
		if __channel.value != null:
			return true
		return false
	func get_channel() -> String:
		return __channel.value
	func clear_channel() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__channel.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_channel(value : String) -> void:
		__channel.value = value
	
	var __sender: PBField
	func has_sender() -> bool:
		if __sender.value != null:
			return true
		return false
	func get_sender() -> String:
		return __sender.value
	func clear_sender() -> void:
		data[2].state = PB_SERVICE_STATE.UNFILLED
		__sender.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_sender(value : String) -> void:
		__sender.value = value
	
	var __text: PBField
	func has_text() -> bool:
		if __text.value != null:
			return true
		return false
	func get_text() -> String:
		return __text.value
	func clear_text() -> void:
		data[3].state = PB_SERVICE_STATE.UNFILLED
		__text.value = DEFAULT_VALUES_3[PB_DATA_TYPE.STRING]
	func set_text(value : String) -> void:
		__text.value = value
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
class ChatChannels:
	func _init():
		var service
		
		var __joined_default: Array[String] = []
		__joined = PBField.new("joined", PB_DATA_TYPE.STRING, PB_RULE.REPEATED, 1, true, __joined_default)
		service = PBServiceField.new()
		service.field = __joined
		data[__joined.tag] = service
		
	var data = {}
	
	var __joined: PBField
	func get_joined() -> Array[String]:
		return __joined.value
	func clear_joined() -> void:
		data[1].state = PB_SERVICE_STATE.UNFILLED
		__joined.value.clear()
	func add_joined(value : String) -> void:
		__joined.value.append(value)
	
	func _to_string() -> String:
		return PBPacker.message_to_string(data)
		
	func to_bytes() -> PackedByteArray:
		return PBPacker.pack_message(data)
		
	func from_bytes(bytes : PackedByteArray, offset : int = 0, limit : int = -1) -> int:
		var cur_limit = bytes.size()
		if limit != -1:
			cur_limit = limit
		var result = PBPacker.unpack_message(data, bytes, offset, cur_limit)
		if result == cur_limit:
			if PBPacker.check_required(data):
				if limit == -1:
					return PB_ERR.NO_ERRORS
			else:
				return PB_ERR.REQUIRED_FIELDS
		elif limit == -1 && result > 0:
			return PB_ERR.PARSE_INCOMPLETE
		return result
	
################ USER DATA END #################
